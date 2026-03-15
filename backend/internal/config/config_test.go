package config

import "testing"

func resetConfigEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"APP_ENV",
		"HTTP_ADDR",
		"DB_PATH",
		"JWT_SECRET",
		"ACCESS_TOKEN_TTL",
		"REFRESH_TOKEN_TTL",
		"AUTH_COOKIE_DOMAIN",
		"AUTH_COOKIE_SECURE",
		"AUTH_COOKIE_SAMESITE",
		"AUTH_COOKIE_PATH",
		"CORS_ALLOWED_ORIGINS",
		"STORAGE_DRIVER",
		"LOCAL_STORAGE_DIR",
		"PUBLIC_BASE_URL",
		"MAX_UPLOAD_MB",
		"UPLOAD_URL_EXPIRES",
		"FFMPEG_BIN",
		"FFPROBE_BIN",
		"YTDLP_BIN",
		"TRANSCODE_POLL_INTERVAL",
		"TRANSCODE_MAX_RETRIES",
		"IMPORT_POLL_INTERVAL",
		"IMPORT_MAX_RETRIES",
		"IMPORT_TORRENT_MAX_MB",
		"IMPORT_MAX_SELECTED_FILES",
		"IMPORT_URL_TIMEOUT_SEC",
		"IMPORT_URL_MAX_DURATION_SEC",
		"IMPORT_URL_MAX_FILE_MB",
		"IMPORT_PAGE_RESOLVER_ENABLED",
		"IMPORT_PAGE_RESOLVER_TIMEOUT_SEC",
		"IMPORT_PAGE_RESOLVER_MAX_CANDIDATES",
		"IMPORT_PAGE_RESOLVER_CMD",
		"RATE_LIMIT_ENABLED",
		"RATE_LIMIT_REDIS_ADDR",
		"RATE_LIMIT_REDIS_PASSWORD",
		"RATE_LIMIT_REDIS_DB",
		"RATE_LIMIT_REDIS_PREFIX",
		"RATE_LIMIT_FAIL_CLOSED_PROD",
		"RATE_LIMIT_DEV_FALLBACK_MEMORY",
		"S3_BUCKET",
		"S3_REGION",
		"S3_ENDPOINT",
		"S3_ACCESS_KEY_ID",
		"S3_SECRET_ACCESS_KEY",
		"S3_SESSION_TOKEN",
		"S3_FORCE_PATH_STYLE",
		"S3_PUBLIC_BASE_URL",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func TestLoadRejectsEmptyJWTSecret(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("JWT_SECRET", "   ")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for empty JWT_SECRET")
	}
}

func TestLoadRejectsPlaceholderJWTSecret(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("JWT_SECRET", defaultJWTSecret)

	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for placeholder JWT_SECRET")
	}
}

func TestLoadAcceptsCustomJWTSecret(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("JWT_SECRET", "my-very-strong-secret-for-tests")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if cfg.JWTSecret != "my-very-strong-secret-for-tests" {
		t.Fatalf("unexpected JWTSecret: %s", cfg.JWTSecret)
	}
}
