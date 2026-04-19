package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	// SQLCipher 드라이버는 sqlite.go에서 이미 import됨
)

// OpenEncryptedDB - SQLCipher 암호화 DB 열기 (provider_keys용)
// dbKey는 SQLCipher의 PRAGMA key로 사용되는 암호화 키
func OpenEncryptedDB(dbPath string, dbKey string) (*sql.DB, error) {
	if dbKey == "" {
		return nil, fmt.Errorf("DB 암호화 키가 설정되지 않았습니다 (EZAI_DB_KEY 환경변수 확인)")
	}

	// 디렉토리 생성
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("DB 디렉토리 생성 실패: %w", err)
	}

	// SQLCipher DSN: 파일 경로에 _pragma_key 쿼리 파라미터로 암호화 키 전달
	dsn := fmt.Sprintf("%s?_pragma_key=%s&_pragma_cipher_page_size=4096",
		dbPath, url.QueryEscape(dbKey))

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("SQLCipher 열기 실패: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// 암호화 키 검증 (잘못된 키면 에러)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("SQLCipher 연결 실패 (키가 올바른지 확인): %w", err)
	}

	// DB 파일 권한 설정 (소유자만 읽기/쓰기)
	if _, statErr := os.Stat(dbPath); statErr == nil {
		if err := os.Chmod(dbPath, 0600); err != nil {
			return nil, fmt.Errorf("DB 파일 권한 설정 실패: %w", err)
		}
	}

	return db, nil
}
