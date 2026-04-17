package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLite/SQLCipher 통합 드라이버
)

// OpenSQLite - 일반 SQLite DB 열기 (request_logs용, 암호화 없음)
func OpenSQLite(dbPath string) (*sql.DB, error) {
	// 디렉토리 생성
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("DB 디렉토리 생성 실패: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("SQLite 열기 실패: %w", err)
	}

	// 연결 풀 설정
	db.SetMaxOpenConns(1) // SQLite는 단일 쓰기
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("SQLite 연결 확인 실패: %w", err)
	}

	return db, nil
}

// RunMigrations - SQL 마이그레이션 파일 실행
// migrationsDir 내의 .sql 파일을 이름 순서대로 실행한다.
func RunMigrations(db *sql.DB, migrationsDir string, fileFilter func(string) bool) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("마이그레이션 디렉토리 읽기 실패: %w", err)
	}

	// .sql 파일만 필터링 후 정렬
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			if fileFilter == nil || fileFilter(e.Name()) {
				files = append(files, e.Name())
			}
		}
	}
	sort.Strings(files)

	for _, f := range files {
		path := filepath.Join(migrationsDir, f)
		sqlBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("마이그레이션 파일 읽기 실패 (%s): %w", f, err)
		}
		if _, err := db.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("마이그레이션 실행 실패 (%s): %w", f, err)
		}
	}

	return nil
}
