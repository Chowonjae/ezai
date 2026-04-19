package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.uber.org/zap"

	"github.com/redis/go-redis/v9"

	"github.com/Chowonjae/ezai/internal/cache"
	"github.com/Chowonjae/ezai/internal/config"
	"github.com/Chowonjae/ezai/internal/crypto"
	"github.com/Chowonjae/ezai/internal/provider"
	"github.com/Chowonjae/ezai/internal/queue"
	"github.com/Chowonjae/ezai/internal/router"
	"github.com/Chowonjae/ezai/internal/server"
	"github.com/Chowonjae/ezai/internal/store"
)

func main() {
	// 서브커맨드 분기
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "keys":
			runKeysCommand(os.Args[2:])
			return
		case "serve":
			// serve는 아래 기본 동작과 동일
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	// 기본: 서버 시작
	runServe()
}

func printUsage() {
	fmt.Println(`ezai - Multi AI Gateway

사용법:
  ezai                서버 시작 (serve와 동일)
  ezai serve          서버 시작
  ezai keys           키 관리 커맨드

키 관리:
  ezai keys add <provider> --value <key>         API 키 등록
  ezai keys add <provider> --file <path>         파일에서 키 등록 (Gemini 서비스 계정 JSON)
  ezai keys list                                 등록된 키 목록 조회
  ezai keys delete <id>                          키 비활성화
  ezai keys rotate <id> --value <new_key>        키 로테이션
  ezai keys init-db                              DB 암호화 키 생성

환경변수:
  EZAI_DB_KEY          DB 암호화 키 (필수, hex 64자)
  EZAI_CONFIG_DIR      설정 디렉토리 (기본: config)`)
}

// ============================================================
// 서버 시작
// ============================================================

func runServe() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("로거 초기화 실패: %v", err)
	}
	defer logger.Sync()

	configDir := os.Getenv("EZAI_CONFIG_DIR")
	if configDir == "" {
		configDir = "config"
	}
	cfg, err := config.Load(configDir)
	if err != nil {
		logger.Fatal("설정 로드 실패", zap.Error(err))
	}

	migrationsDir := "migrations"

	// 일반 SQLite (request_logs)
	logsDB, err := store.OpenSQLite(cfg.Database.LogsPath)
	if err != nil {
		logger.Fatal("로그 DB 초기화 실패", zap.Error(err))
	}
	defer logsDB.Close()

	if err := store.RunMigrations(logsDB, migrationsDir, func(name string) bool {
		return strings.HasPrefix(name, "003_") || strings.HasPrefix(name, "004_")
	}); err != nil {
		logger.Fatal("로그 DB 마이그레이션 실패", zap.Error(err))
	}
	logger.Info("로그 DB 초기화 완료", zap.String("path", cfg.Database.LogsPath))

	logWriter := store.NewRequestLogWriter(logsDB, logger, 1000)

	// 암호화 DB (provider_keys + client_keys)
	var keyStore *store.KeyStore
	var auditLog *store.AuditLog
	var clientKeyStore *store.ClientKeyStore
	dbKey := os.Getenv("EZAI_DB_KEY")
	if dbKey != "" {
		encryptor, err := crypto.NewEncryptor(dbKey)
		if err != nil {
			logger.Fatal("암호화기 초기화 실패", zap.Error(err))
		}
		keysDB, err := store.OpenEncryptedDB(cfg.Database.KeysPath, dbKey)
		if err != nil {
			logger.Fatal("키 DB 초기화 실패", zap.Error(err))
		}
		defer keysDB.Close()

		if err := store.RunMigrations(keysDB, migrationsDir, func(name string) bool {
			return strings.HasPrefix(name, "001_") || strings.HasPrefix(name, "002_") || strings.HasPrefix(name, "005_")
		}); err != nil {
			logger.Fatal("키 DB 마이그레이션 실패", zap.Error(err))
		}

		keyStore = store.NewKeyStore(keysDB, encryptor)
		auditLog = store.NewAuditLog(keysDB)
		clientKeyStore = store.NewClientKeyStore(keysDB)
		logger.Info("키 DB 초기화 완료 (암호화)", zap.String("path", cfg.Database.KeysPath))
	} else {
		logger.Warn("EZAI_DB_KEY 미설정: API 키 관리, 감사 로그, 클라이언트 키 인증 비활성화")
	}

	// 프로바이더 레지스트리
	registry := provider.NewRegistry()
	ctx := context.Background()
	registerProviders(ctx, cfg, registry, keyStore, logger)

	// 라우터
	var rt *router.Router
	if cfg.Fallback != nil {
		rt = router.NewRouter(registry, cfg.Fallback, logger)
		logger.Info("라우터 초기화 완료 (fallback/circuit breaker 활성화)")
	}

	// 가격 테이블
	pricingManager, err := config.NewPricingManager(configDir)
	if err != nil {
		logger.Warn("가격 테이블 로드 실패", zap.Error(err))
	} else {
		logger.Info("가격 테이블 로드 완료")
	}

	usageReader := store.NewUsageReader(logsDB)
	usageAdmin := store.NewUsageAdmin(logsDB)

	// 보존 정책 로드
	retentionCfg, err := config.LoadRetentionConfig(configDir)
	if err != nil {
		logger.Warn("보존 정책 설정 로드 실패 (기본값 사용)", zap.Error(err))
		retentionCfg = &config.RetentionConfig{
			Retention: config.RetentionPolicy{
				DetailLogs: config.DetailLogsRetention{
					HotStorageDays:   90,
					ArchiveAfterDays: 90,
					DeleteAfterDays:  365,
				},
				Reset: config.ResetPolicy{
					RequireConfirmation: true,
					AllowedOperations:   []string{"soft_reset", "archive"},
				},
			},
		}
	} else {
		logger.Info("보존 정책 설정 로드 완료")
	}

	// 로깅 설정 로드
	loggingConfig := config.LoadLoggingConfig(configDir)
	logger.Info("로깅 설정 로드 완료")

	// 프롬프트 매니저
	promptsDir := os.Getenv("EZAI_PROMPTS_DIR")
	if promptsDir == "" {
		promptsDir = "prompts"
	}
	promptManager := config.NewPromptManager(promptsDir)
	logger.Info("프롬프트 매니저 초기화 완료", zap.String("dir", promptsDir))

	// Redis + 캐시
	var rdb *redis.Client
	var respCache *cache.Cache
	var producer *queue.Producer
	var jobStore *queue.JobStore
	var consumer *queue.Consumer

	rdb = redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Warn("Redis 연결 실패: 캐시, 배치 처리, Rate Limiting 비활성화", zap.Error(err))
		rdb = nil
	} else {
		logger.Info("Redis 연결 완료", zap.String("addr", cfg.Redis.Addr))
		respCache = cache.NewCache(rdb, 0) // 기본 TTL 10분
		jobStore = queue.NewJobStore(rdb)
		producer = queue.NewProducer(rdb, jobStore)
		consumer = queue.NewConsumer(rdb, registry, jobStore, logger, 3)
		if rt != nil {
			consumer.SetRouter(rt)
		}
		consumer.Start(ctx)
		logger.Info("배치 Consumer 시작 (workers: 3)")
	}

	// 서버 시작
	srv := server.New(server.Deps{
		Config:         cfg,
		Registry:       registry,
		Router:         rt,
		Logger:         logger,
		KeyStore:       keyStore,
		AuditLog:       auditLog,
		LogWriter:      logWriter,
		PromptManager:  promptManager,
		PricingManager: pricingManager,
		UsageReader:    usageReader,
		Cache:          respCache,
		Redis:          rdb,
		Producer:       producer,
		JobStore:       jobStore,
		Consumer:        consumer,
		UsageAdmin:      usageAdmin,
		RetentionConfig: retentionCfg,
		ClientKeyStore:  clientKeyStore,
		LoggingConfig:   loggingConfig,
		ConfigDir:       configDir,
	})

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("종료 시그널 수신")
		if err := srv.Shutdown(); err != nil {
			logger.Error("서버 종료 실패", zap.Error(err))
		}
	}()

	if err := srv.Start(); err != nil {
		logger.Fatal("서버 실행 실패", zap.Error(err))
	}
}

// ============================================================
// 키 관리 CLI
// ============================================================

func runKeysCommand(args []string) {
	if len(args) == 0 {
		fmt.Println(`사용법:
  ezai keys add <provider> --value <key>       API 키 등록
  ezai keys add <provider> --file <path>       파일에서 키 등록 (Gemini 서비스 계정 JSON)
  ezai keys list                               등록된 키 목록
  ezai keys delete <id>                        키 비활성화
  ezai keys rotate <id> --value <new_key>      키 로테이션
  ezai keys init-db                            DB 암호화 키 생성 (.env에 저장)`)
		return
	}

	switch args[0] {
	case "init-db":
		runKeysInitDB()
	case "add":
		runKeysAdd(args[1:])
	case "list":
		runKeysList()
	case "delete":
		runKeysDelete(args[1:])
	case "rotate":
		runKeysRotate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "알 수 없는 키 커맨드: %s\n", args[0])
		os.Exit(1)
	}
}

// init-db: DB 암호화 키 생성 → .env 파일에 저장
func runKeysInitDB() {
	envFile := ".env"

	// 이미 .env에 EZAI_DB_KEY가 있는지 확인
	if data, err := os.ReadFile(envFile); err == nil {
		if strings.Contains(string(data), "EZAI_DB_KEY=") {
			// 값이 비어있는지 확인
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "EZAI_DB_KEY=") {
					val := strings.TrimPrefix(line, "EZAI_DB_KEY=")
					if val != "" {
						fmt.Println("EZAI_DB_KEY가 이미 설정되어 있습니다.")
						return
					}
				}
			}
		}
	}

	key, err := crypto.GenerateKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "키 생성 실패: %v\n", err)
		os.Exit(1)
	}

	// .env 파일에 추가/생성
	content := fmt.Sprintf("export EZAI_DB_KEY=%s\n", key)
	if data, err := os.ReadFile(envFile); err == nil {
		// 기존 파일에 추가
		existing := string(data)
		if strings.Contains(existing, "EZAI_DB_KEY=") {
			// 기존 빈 값에 키 채우기
			existing = strings.Replace(existing, "EZAI_DB_KEY=\n", "EZAI_DB_KEY="+key+"\n", 1)
			existing = strings.Replace(existing, "EZAI_DB_KEY= ", "EZAI_DB_KEY="+key+" ", 1)
			content = existing
		} else {
			content = existing + content
		}
	}

	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		fmt.Fprintf(os.Stderr, ".env 파일 쓰기 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("DB 암호화 키 생성 완료 → %s\n", envFile)
	fmt.Println("이제 키를 등록하세요:")
	fmt.Println("  source .env && ezai keys add gemini --file /path/to/sa.json")
	fmt.Println("  source .env && ezai keys add claude --value sk-ant-...")
}

// add: 키 등록
func runKeysAdd(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "사용법: ezai keys add <provider> --value <key> 또는 --file <path>")
		os.Exit(1)
	}

	providerName := args[0]
	var keyValue string
	var keyType string

	// 옵션 파싱
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--value", "-v":
			if i+1 < len(args) {
				keyValue = args[i+1]
				keyType = "api_key"
				i++
			}
		case "--file", "-f":
			if i+1 < len(args) {
				data, err := os.ReadFile(args[i+1])
				if err != nil {
					fmt.Fprintf(os.Stderr, "파일 읽기 실패: %v\n", err)
					os.Exit(1)
				}
				keyValue = string(data)
				keyType = "service_account_json"
				i++
			}
		case "--type", "-t":
			if i+1 < len(args) {
				keyType = args[i+1]
				i++
			}
		}
	}

	if keyValue == "" {
		fmt.Fprintln(os.Stderr, "키 값을 지정하세요: --value <key> 또는 --file <path>")
		os.Exit(1)
	}
	if keyType == "" {
		keyType = "api_key"
	}

	// keys.db에 등록 (동일 provider가 있으면 자동 업데이트)
	ks, auditLog := openKeyStore()

	key, err := ks.Create(providerName, "main", keyValue, keyType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "키 등록 실패: %v\n", err)
		os.Exit(1)
	}

	_ = auditLog.Record(key.ID, "created", "cli", "CLI로 키 등록/업데이트")

	fmt.Printf("키 등록 완료:\n")
	fmt.Printf("  ID:       %d\n", key.ID)
	fmt.Printf("  Provider: %s\n", key.Provider)
	fmt.Printf("  Type:     %s\n", key.KeyType)
}

// list: 키 목록
func runKeysList() {
	ks, _ := openKeyStore()

	keys, err := ks.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "키 목록 조회 실패: %v\n", err)
		os.Exit(1)
	}

	if len(keys) == 0 {
		fmt.Println("등록된 키가 없습니다.")
		return
	}

	fmt.Printf("%-4s %-12s %-12s %-22s %-8s\n", "ID", "Provider", "Type", "KeyName", "Active")
	fmt.Println(strings.Repeat("-", 62))
	for _, k := range keys {
		active := "✓"
		if !k.IsActive {
			active = "✗"
		}
		fmt.Printf("%-4d %-12s %-12s %-22s %-8s\n", k.ID, k.Provider, k.KeyType, k.KeyName, active)
	}
}

// delete: 키 비활성화
func runKeysDelete(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "사용법: ezai keys delete <id>")
		os.Exit(1)
	}

	var id int64
	fmt.Sscanf(args[0], "%d", &id)

	ks, auditLog := openKeyStore()

	if err := ks.Deactivate(id); err != nil {
		fmt.Fprintf(os.Stderr, "키 비활성화 실패: %v\n", err)
		os.Exit(1)
	}

	_ = auditLog.Record(id, "deactivated", "cli", "CLI로 키 비활성화")
	fmt.Printf("키 #%d 비활성화 완료\n", id)
}

// rotate: 키 로테이션
func runKeysRotate(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "사용법: ezai keys rotate <id> --value <new_key> 또는 --file <path>")
		os.Exit(1)
	}

	var id int64
	fmt.Sscanf(args[0], "%d", &id)

	var newValue string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--value", "-v":
			if i+1 < len(args) {
				newValue = args[i+1]
				i++
			}
		case "--file", "-f":
			if i+1 < len(args) {
				data, err := os.ReadFile(args[i+1])
				if err != nil {
					fmt.Fprintf(os.Stderr, "파일 읽기 실패: %v\n", err)
					os.Exit(1)
				}
				newValue = string(data)
				i++
			}
		}
	}

	if newValue == "" {
		fmt.Fprintln(os.Stderr, "새 키 값을 지정하세요: --value <key> 또는 --file <path>")
		os.Exit(1)
	}

	ks, auditLog := openKeyStore()

	newKey, err := ks.Rotate(id, newValue)
	if err != nil {
		fmt.Fprintf(os.Stderr, "키 로테이션 실패: %v\n", err)
		os.Exit(1)
	}

	_ = auditLog.Record(id, "rotated", "cli", "CLI로 키 로테이션")
	_ = auditLog.Record(newKey.ID, "created", "cli", "로테이션으로 생성된 새 키")
	fmt.Printf("키 로테이션 완료: #%d(비활성화) → #%d(신규)\n", id, newKey.ID)
}

// openKeyStore - CLI 커맨드용 keys.db 열기
func openKeyStore() (*store.KeyStore, *store.AuditLog) {
	dbKey := os.Getenv("EZAI_DB_KEY")
	if dbKey == "" {
		fmt.Fprintln(os.Stderr, "EZAI_DB_KEY 환경변수가 설정되지 않았습니다.")
		fmt.Fprintln(os.Stderr, "  먼저 실행: ezai keys init-db && source .env")
		os.Exit(1)
	}

	configDir := os.Getenv("EZAI_CONFIG_DIR")
	if configDir == "" {
		configDir = "config"
	}
	cfg, err := config.Load(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "설정 로드 실패: %v\n", err)
		os.Exit(1)
	}

	encryptor, err := crypto.NewEncryptor(dbKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "암호화기 초기화 실패: %v\n", err)
		os.Exit(1)
	}

	keysDB, err := store.OpenEncryptedDB(cfg.Database.KeysPath, dbKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "키 DB 열기 실패: %v\n", err)
		os.Exit(1)
	}

	// 마이그레이션
	migrationsDir := "migrations"
	if err := store.RunMigrations(keysDB, migrationsDir, func(name string) bool {
		return strings.HasPrefix(name, "001_") || strings.HasPrefix(name, "002_") || strings.HasPrefix(name, "005_")
	}); err != nil {
		fmt.Fprintf(os.Stderr, "마이그레이션 실패: %v\n", err)
		os.Exit(1)
	}

	return store.NewKeyStore(keysDB, encryptor), store.NewAuditLog(keysDB)
}

// ============================================================
// 프로바이더 등록
// ============================================================

// resolveKey - keys.db에서 키 조회, 없으면 환경변수 fallback
// resolveKeyResult - API 키 조회 결과
type resolveKeyResult struct {
	Key   string
	KeyID int64
}

func resolveKey(keyStore *store.KeyStore, providerName, envVar string, logger *zap.Logger) resolveKeyResult {
	if keyStore != nil {
		if id, key, err := keyStore.GetActiveByProviderWithID(providerName); err == nil && key != "" {
			logger.Info("키 로드 (keys.db)", zap.String("provider", providerName))
			return resolveKeyResult{Key: key, KeyID: id}
		}
	}
	if val := os.Getenv(envVar); val != "" {
		logger.Info("키 로드 (환경변수)", zap.String("provider", providerName), zap.String("env", envVar))
		return resolveKeyResult{Key: val}
	}
	return resolveKeyResult{}
}

// registerProviders - 설정에 따라 프로바이더를 레지스트리에 등록
func registerProviders(ctx context.Context, cfg *config.Config, registry *provider.Registry, keyStore *store.KeyStore, logger *zap.Logger) {
	// Gemini (Vertex AI)
	if provCfg, ok := cfg.Providers["gemini"]; ok && provCfg.Enabled {
		location := provCfg.Location
		if location == "" {
			location = "us-central1"
		}
		saJSON := ""
		var geminiKeyID int64
		if keyStore != nil {
			if id, val, err := keyStore.GetActiveByProviderWithID("gemini"); err == nil && val != "" {
				saJSON = val
				geminiKeyID = id
				logger.Info("키 로드 (keys.db)", zap.String("provider", "gemini"))
			}
		}
		if saJSON == "" {
			if credPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credPath != "" {
				if data, err := os.ReadFile(credPath); err == nil {
					saJSON = string(data)
					logger.Info("키 로드 (환경변수 파일)", zap.String("provider", "gemini"))
				}
			}
		}

		projectID := os.Getenv("EZAI_GEMINI_PROJECT")
		gemini, err := provider.NewGeminiProvider(ctx, provider.GeminiConfig{
			ProjectID:          projectID,
			Location:           location,
			ServiceAccountJSON: saJSON,
		})
		if err != nil {
			logger.Warn("Gemini 프로바이더 초기화 실패", zap.Error(err))
		} else {
			registry.RegisterWithKeyID(gemini, geminiKeyID)
			logger.Info("프로바이더 등록", zap.String("provider", "gemini"))
		}
	}

	// Claude (Anthropic)
	if provCfg, ok := cfg.Providers["claude"]; ok && provCfg.Enabled {
		resolved := resolveKey(keyStore, "claude", "ANTHROPIC_API_KEY", logger)
		claude, err := provider.NewClaudeProvider(resolved.Key)
		if err != nil {
			logger.Warn("Claude 프로바이더 초기화 실패", zap.Error(err))
		} else {
			registry.RegisterWithKeyID(claude, resolved.KeyID)
			logger.Info("프로바이더 등록", zap.String("provider", "claude"))
		}
	}

	// GPT (OpenAI)
	if provCfg, ok := cfg.Providers["gpt"]; ok && provCfg.Enabled {
		resolved := resolveKey(keyStore, "gpt", "OPENAI_API_KEY", logger)
		gpt, err := provider.NewGPTProvider(resolved.Key)
		if err != nil {
			logger.Warn("GPT 프로바이더 초기화 실패", zap.Error(err))
		} else {
			registry.RegisterWithKeyID(gpt, resolved.KeyID)
			logger.Info("프로바이더 등록", zap.String("provider", "gpt"))
		}
	}

	// Perplexity
	if provCfg, ok := cfg.Providers["perplexity"]; ok && provCfg.Enabled {
		resolved := resolveKey(keyStore, "perplexity", "PERPLEXITY_API_KEY", logger)
		perplexity, err := provider.NewPerplexityProvider(resolved.Key, provCfg.BaseURL)
		if err != nil {
			logger.Warn("Perplexity 프로바이더 초기화 실패", zap.Error(err))
		} else {
			registry.RegisterWithKeyID(perplexity, resolved.KeyID)
			logger.Info("프로바이더 등록", zap.String("provider", "perplexity"))
		}
	}

	// Ollama (로컬 LLM, 키 불필요)
	if provCfg, ok := cfg.Providers["ollama"]; ok && provCfg.Enabled {
		ollama, err := provider.NewOllamaProvider(provCfg.BaseURL)
		if err != nil {
			logger.Warn("Ollama 프로바이더 초기화 실패", zap.Error(err))
		} else {
			registry.Register(ollama)
			logger.Info("프로바이더 등록", zap.String("provider", "ollama"))
		}
	}
}
