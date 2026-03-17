package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env                          string
	LogLevel                     string
	HTTPAddr                     string
	DBPath                       string
	JWTSecret                    string
	AccessTokenTTL               time.Duration
	RefreshTokenTTL              time.Duration
	AuthCookieDomain             string
	AuthCookieSecure             bool
	AuthCookieSameSite           string
	AuthCookiePath               string
	CORSAllowedOrigins           []string
	StorageDriver                string
	LocalStorageDir              string
	TaskTempDir                  string
	PublicBaseURL                string
	MaxUploadBytes               int64
	UploadURLExpires             time.Duration
	FFmpegBin                    string
	FFprobeBin                   string
	YTDLPBin                     string
	TranscodePoll                time.Duration
	TranscodeMaxTry              int
	ImportPoll                   time.Duration
	ImportMaxTry                 int
	ImportTorrentMax             int64
	ImportMaxFiles               int
	ImportBTEnableUpload         bool
	ImportBTListenPort           int
	ImportBTEnablePortForward    bool
	ImportBTReaderReadaheadBytes int64
	ImportBTSpeedSmoothWindowSec int
	ImportURLTimeout             time.Duration
	ImportURLMaxDur              int64
	ImportURLMaxFile             int64
	ImportPageResolverEnabled    bool
	ImportPageResolverTimeout    time.Duration
	ImportPageResolverMax        int
	ImportPageResolverCmd        string
	ImportForceFallbackDomains   []string
	ImportProgressLogInterval    time.Duration
	TranscodeProgressLogInterval time.Duration
	RateLimitEnabled             bool
	RateLimitRedisAddr           string
	RateLimitRedisPassword       string
	RateLimitRedisDB             int
	RateLimitRedisPrefix         string
	RateLimitFailClosedProd      bool
	RateLimitDevFallbackMem      bool

	S3Bucket          string
	S3Region          string
	S3Endpoint        string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3SessionToken    string
	S3ForcePathStyle  bool
	S3PublicBaseURL   string
}

const defaultJWTSecret = "change-me-in-production"

var jwtSecretPlaceholders = map[string]struct{}{
	"default":                 {},
	"change-me":               {},
	defaultJWTSecret:          {},
	"replace-with-strong-key": {},
}

func Load() (Config, error) {
	cfg := Config{
		Env:              getEnv("APP_ENV", "development"),
		LogLevel:         strings.ToLower(strings.TrimSpace(getEnv("LOG_LEVEL", "info"))),
		HTTPAddr:         getEnv("HTTP_ADDR", ":8080"),
		DBPath:           getEnv("DB_PATH", "./data/moevideo.db"),
		JWTSecret:        getEnv("JWT_SECRET", defaultJWTSecret),
		AuthCookieDomain: strings.TrimSpace(getEnv("AUTH_COOKIE_DOMAIN", "")),
		AuthCookiePath:   getEnv("AUTH_COOKIE_PATH", "/"),
		AuthCookieSameSite: strings.ToLower(
			strings.TrimSpace(getEnv("AUTH_COOKIE_SAMESITE", "lax")),
		),
		StorageDriver:     strings.ToLower(getEnv("STORAGE_DRIVER", "local")),
		LocalStorageDir:   getEnv("LOCAL_STORAGE_DIR", "./storage/local"),
		TaskTempDir:       strings.TrimSpace(getEnv("TASK_TEMP_DIR", "./data/temp")),
		PublicBaseURL:     strings.TrimRight(getEnv("PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		S3Bucket:          getEnv("S3_BUCKET", ""),
		S3Region:          getEnv("S3_REGION", ""),
		S3Endpoint:        getEnv("S3_ENDPOINT", ""),
		S3AccessKeyID:     getEnv("S3_ACCESS_KEY_ID", ""),
		S3SecretAccessKey: getEnv("S3_SECRET_ACCESS_KEY", ""),
		S3SessionToken:    getEnv("S3_SESSION_TOKEN", ""),
		S3PublicBaseURL:   strings.TrimRight(getEnv("S3_PUBLIC_BASE_URL", ""), "/"),
		FFmpegBin:         getEnv("FFMPEG_BIN", "ffmpeg"),
		FFprobeBin:        getEnv("FFPROBE_BIN", "ffprobe"),
		YTDLPBin:          getEnv("YTDLP_BIN", "yt-dlp"),
		ImportPageResolverCmd: getEnv(
			"IMPORT_PAGE_RESOLVER_CMD",
			"bun scripts/page_manifest_resolver.mjs",
		),
		RateLimitRedisAddr:     strings.TrimSpace(getEnv("RATE_LIMIT_REDIS_ADDR", "")),
		RateLimitRedisPassword: getEnv("RATE_LIMIT_REDIS_PASSWORD", ""),
		RateLimitRedisPrefix:   strings.TrimSpace(getEnv("RATE_LIMIT_REDIS_PREFIX", "moevideo")),
	}
	if err := validateJWTSecret(cfg.JWTSecret); err != nil {
		return cfg, err
	}

	cookieSecureRaw := strings.ToLower(strings.TrimSpace(getEnv("AUTH_COOKIE_SECURE", "")))
	switch cookieSecureRaw {
	case "1", "true", "yes":
		cfg.AuthCookieSecure = true
	case "0", "false", "no":
		cfg.AuthCookieSecure = false
	default:
		cfg.AuthCookieSecure = cfg.Env == "production"
	}
	if cfg.Env == "production" {
		cfg.AuthCookieSecure = true
	}

	switch cfg.AuthCookieSameSite {
	case "strict", "lax", "none":
	default:
		return cfg, fmt.Errorf("invalid AUTH_COOKIE_SAMESITE: %s", cfg.AuthCookieSameSite)
	}
	if cfg.AuthCookiePath == "" {
		cfg.AuthCookiePath = "/"
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return cfg, fmt.Errorf("invalid LOG_LEVEL: %s", cfg.LogLevel)
	}
	if cfg.TaskTempDir == "" {
		return cfg, fmt.Errorf("TASK_TEMP_DIR must not be empty")
	}
	cfg.CORSAllowedOrigins = parseCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000"))

	rateLimitEnabledRaw := strings.ToLower(strings.TrimSpace(getEnv("RATE_LIMIT_ENABLED", "true")))
	cfg.RateLimitEnabled = rateLimitEnabledRaw == "1" || rateLimitEnabledRaw == "true" || rateLimitEnabledRaw == "yes"
	if cfg.RateLimitRedisPrefix == "" {
		cfg.RateLimitRedisPrefix = "moevideo"
	}
	rateLimitRedisDB, err := strconv.Atoi(getEnv("RATE_LIMIT_REDIS_DB", "0"))
	if err != nil {
		return cfg, fmt.Errorf("invalid RATE_LIMIT_REDIS_DB: %w", err)
	}
	if rateLimitRedisDB < 0 {
		return cfg, fmt.Errorf("RATE_LIMIT_REDIS_DB must be non-negative")
	}
	cfg.RateLimitRedisDB = rateLimitRedisDB
	failClosedRaw := strings.ToLower(strings.TrimSpace(getEnv("RATE_LIMIT_FAIL_CLOSED_PROD", "true")))
	cfg.RateLimitFailClosedProd = failClosedRaw == "1" || failClosedRaw == "true" || failClosedRaw == "yes"
	devFallbackRaw := strings.ToLower(strings.TrimSpace(getEnv("RATE_LIMIT_DEV_FALLBACK_MEMORY", "true")))
	cfg.RateLimitDevFallbackMem = devFallbackRaw == "1" || devFallbackRaw == "true" || devFallbackRaw == "yes"

	accessTTL, err := time.ParseDuration(getEnv("ACCESS_TOKEN_TTL", "15m"))
	if err != nil {
		return cfg, fmt.Errorf("invalid ACCESS_TOKEN_TTL: %w", err)
	}
	cfg.AccessTokenTTL = accessTTL

	refreshTTL, err := time.ParseDuration(getEnv("REFRESH_TOKEN_TTL", "720h"))
	if err != nil {
		return cfg, fmt.Errorf("invalid REFRESH_TOKEN_TTL: %w", err)
	}
	cfg.RefreshTokenTTL = refreshTTL

	uploadURLExpires, err := time.ParseDuration(getEnv("UPLOAD_URL_EXPIRES", "15m"))
	if err != nil {
		return cfg, fmt.Errorf("invalid UPLOAD_URL_EXPIRES: %w", err)
	}
	cfg.UploadURLExpires = uploadURLExpires

	transcodePoll, err := time.ParseDuration(getEnv("TRANSCODE_POLL_INTERVAL", "1s"))
	if err != nil {
		return cfg, fmt.Errorf("invalid TRANSCODE_POLL_INTERVAL: %w", err)
	}
	cfg.TranscodePoll = transcodePoll

	transcodeMaxTry, err := strconv.Atoi(getEnv("TRANSCODE_MAX_RETRIES", "3"))
	if err != nil {
		return cfg, fmt.Errorf("invalid TRANSCODE_MAX_RETRIES: %w", err)
	}
	if transcodeMaxTry <= 0 {
		return cfg, fmt.Errorf("TRANSCODE_MAX_RETRIES must be positive")
	}
	cfg.TranscodeMaxTry = transcodeMaxTry

	importPoll, err := time.ParseDuration(getEnv("IMPORT_POLL_INTERVAL", "1s"))
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_POLL_INTERVAL: %w", err)
	}
	cfg.ImportPoll = importPoll

	importMaxTry, err := strconv.Atoi(getEnv("IMPORT_MAX_RETRIES", "3"))
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_MAX_RETRIES: %w", err)
	}
	if importMaxTry <= 0 {
		return cfg, fmt.Errorf("IMPORT_MAX_RETRIES must be positive")
	}
	cfg.ImportMaxTry = importMaxTry

	importTorrentMaxMB, err := strconv.ParseInt(getEnv("IMPORT_TORRENT_MAX_MB", "2"), 10, 64)
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_TORRENT_MAX_MB: %w", err)
	}
	if importTorrentMaxMB <= 0 {
		return cfg, fmt.Errorf("IMPORT_TORRENT_MAX_MB must be positive")
	}
	cfg.ImportTorrentMax = importTorrentMaxMB * 1024 * 1024

	importMaxFiles, err := strconv.Atoi(getEnv("IMPORT_MAX_SELECTED_FILES", "20"))
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_MAX_SELECTED_FILES: %w", err)
	}
	if importMaxFiles <= 0 {
		return cfg, fmt.Errorf("IMPORT_MAX_SELECTED_FILES must be positive")
	}
	cfg.ImportMaxFiles = importMaxFiles

	importBTEnableUploadRaw := strings.ToLower(strings.TrimSpace(getEnv("IMPORT_BT_ENABLE_UPLOAD", "")))
	switch importBTEnableUploadRaw {
	case "1", "true", "yes":
		cfg.ImportBTEnableUpload = true
	case "0", "false", "no":
		cfg.ImportBTEnableUpload = false
	default:
		cfg.ImportBTEnableUpload = cfg.Env == "production"
	}

	importBTListenPort, err := strconv.Atoi(getEnv("IMPORT_BT_LISTEN_PORT", "51413"))
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_BT_LISTEN_PORT: %w", err)
	}
	if importBTListenPort < 0 || importBTListenPort > 65535 {
		return cfg, fmt.Errorf("IMPORT_BT_LISTEN_PORT must be between 0 and 65535")
	}
	cfg.ImportBTListenPort = importBTListenPort

	importBTEnablePortForwardRaw := strings.ToLower(strings.TrimSpace(getEnv("IMPORT_BT_ENABLE_PORT_FORWARD", "true")))
	cfg.ImportBTEnablePortForward = importBTEnablePortForwardRaw == "1" || importBTEnablePortForwardRaw == "true" || importBTEnablePortForwardRaw == "yes"

	importBTReaderReadaheadMB, err := strconv.Atoi(getEnv("IMPORT_BT_READER_READAHEAD_MB", "32"))
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_BT_READER_READAHEAD_MB: %w", err)
	}
	if importBTReaderReadaheadMB <= 0 {
		return cfg, fmt.Errorf("IMPORT_BT_READER_READAHEAD_MB must be positive")
	}
	cfg.ImportBTReaderReadaheadBytes = int64(importBTReaderReadaheadMB) * 1024 * 1024

	importBTSpeedSmoothWindowSec, err := strconv.Atoi(getEnv("IMPORT_BT_SPEED_SMOOTH_WINDOW_SEC", "5"))
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_BT_SPEED_SMOOTH_WINDOW_SEC: %w", err)
	}
	if importBTSpeedSmoothWindowSec <= 0 {
		return cfg, fmt.Errorf("IMPORT_BT_SPEED_SMOOTH_WINDOW_SEC must be positive")
	}
	cfg.ImportBTSpeedSmoothWindowSec = importBTSpeedSmoothWindowSec

	importURLTimeoutSec, err := strconv.ParseInt(getEnv("IMPORT_URL_TIMEOUT_SEC", "600"), 10, 64)
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_URL_TIMEOUT_SEC: %w", err)
	}
	if importURLTimeoutSec <= 0 {
		return cfg, fmt.Errorf("IMPORT_URL_TIMEOUT_SEC must be positive")
	}
	cfg.ImportURLTimeout = time.Duration(importURLTimeoutSec) * time.Second

	importURLMaxDurSec, err := strconv.ParseInt(getEnv("IMPORT_URL_MAX_DURATION_SEC", "0"), 10, 64)
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_URL_MAX_DURATION_SEC: %w", err)
	}
	if importURLMaxDurSec < 0 {
		return cfg, fmt.Errorf("IMPORT_URL_MAX_DURATION_SEC must be non-negative")
	}
	cfg.ImportURLMaxDur = importURLMaxDurSec

	importURLMaxFileMB, err := strconv.ParseInt(getEnv("IMPORT_URL_MAX_FILE_MB", "0"), 10, 64)
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_URL_MAX_FILE_MB: %w", err)
	}
	if importURLMaxFileMB < 0 {
		return cfg, fmt.Errorf("IMPORT_URL_MAX_FILE_MB must be non-negative")
	}
	cfg.ImportURLMaxFile = importURLMaxFileMB * 1024 * 1024

	importPageResolverEnabled := strings.ToLower(getEnv("IMPORT_PAGE_RESOLVER_ENABLED", "true"))
	cfg.ImportPageResolverEnabled = importPageResolverEnabled == "1" || importPageResolverEnabled == "true" || importPageResolverEnabled == "yes"

	importPageResolverTimeoutSec, err := strconv.ParseInt(getEnv("IMPORT_PAGE_RESOLVER_TIMEOUT_SEC", "25"), 10, 64)
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_PAGE_RESOLVER_TIMEOUT_SEC: %w", err)
	}
	if importPageResolverTimeoutSec <= 0 {
		return cfg, fmt.Errorf("IMPORT_PAGE_RESOLVER_TIMEOUT_SEC must be positive")
	}
	cfg.ImportPageResolverTimeout = time.Duration(importPageResolverTimeoutSec) * time.Second

	importPageResolverMax, err := strconv.Atoi(getEnv("IMPORT_PAGE_RESOLVER_MAX_CANDIDATES", "20"))
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_PAGE_RESOLVER_MAX_CANDIDATES: %w", err)
	}
	if importPageResolverMax <= 0 {
		return cfg, fmt.Errorf("IMPORT_PAGE_RESOLVER_MAX_CANDIDATES must be positive")
	}
	cfg.ImportPageResolverMax = importPageResolverMax

	cfg.ImportForceFallbackDomains = parseFallbackDomains(getEnv("IMPORT_FORCE_FALLBACK_DOMAINS", ""))

	importProgressLogInterval, err := time.ParseDuration(getEnv("IMPORT_PROGRESS_LOG_INTERVAL", "5s"))
	if err != nil {
		return cfg, fmt.Errorf("invalid IMPORT_PROGRESS_LOG_INTERVAL: %w", err)
	}
	if importProgressLogInterval <= 0 {
		return cfg, fmt.Errorf("IMPORT_PROGRESS_LOG_INTERVAL must be positive")
	}
	cfg.ImportProgressLogInterval = importProgressLogInterval

	transcodeProgressLogInterval, err := time.ParseDuration(getEnv("TRANSCODE_PROGRESS_LOG_INTERVAL", "5s"))
	if err != nil {
		return cfg, fmt.Errorf("invalid TRANSCODE_PROGRESS_LOG_INTERVAL: %w", err)
	}
	if transcodeProgressLogInterval <= 0 {
		return cfg, fmt.Errorf("TRANSCODE_PROGRESS_LOG_INTERVAL must be positive")
	}
	cfg.TranscodeProgressLogInterval = transcodeProgressLogInterval

	maxUploadMB, err := strconv.ParseInt(getEnv("MAX_UPLOAD_MB", "2048"), 10, 64)
	if err != nil {
		return cfg, fmt.Errorf("invalid MAX_UPLOAD_MB: %w", err)
	}
	cfg.MaxUploadBytes = maxUploadMB * 1024 * 1024

	forcePathStyle := strings.ToLower(getEnv("S3_FORCE_PATH_STYLE", "false"))
	cfg.S3ForcePathStyle = forcePathStyle == "1" || forcePathStyle == "true" || forcePathStyle == "yes"

	if cfg.StorageDriver != "local" && cfg.StorageDriver != "s3" {
		return cfg, fmt.Errorf("unsupported STORAGE_DRIVER: %s", cfg.StorageDriver)
	}
	if cfg.StorageDriver == "s3" {
		if cfg.S3Bucket == "" || cfg.S3Region == "" {
			return cfg, fmt.Errorf("S3_BUCKET and S3_REGION are required when STORAGE_DRIVER=s3")
		}
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func validateJWTSecret(raw string) error {
	secret := strings.TrimSpace(raw)
	if secret == "" {
		return fmt.Errorf("JWT_SECRET must be set and must not use placeholder value")
	}
	if _, blocked := jwtSecretPlaceholders[strings.ToLower(secret)]; blocked {
		return fmt.Errorf("JWT_SECRET must be set and must not use placeholder value")
	}
	return nil
}

func parseFallbackDomains(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		normalized, ok := normalizeFallbackDomain(part)
		if !ok {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				fmt.Fprintf(os.Stderr, "warn: ignoring invalid IMPORT_FORCE_FALLBACK_DOMAINS entry: %q\n", trimmed)
			}
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func normalizeFallbackDomain(raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", false
	}
	value = strings.TrimSuffix(value, ".")
	if strings.HasPrefix(value, "*.") {
		value = strings.TrimPrefix(value, "*.")
	}
	if value == "" || strings.Contains(value, "/") {
		return "", false
	}
	if strings.Contains(value, ":") {
		host, _, err := net.SplitHostPort(value)
		if err != nil || strings.TrimSpace(host) == "" {
			return "", false
		}
		value = host
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, label := range labels {
		if label == "" {
			return "", false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return "", false
		}
	}
	return value, true
}

func IsHostMatchedByDomainList(host string, domains []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(domains) == 0 {
		return false
	}
	if strings.Contains(host, ":") {
		parsedHost, _, err := net.SplitHostPort(host)
		if err == nil && strings.TrimSpace(parsedHost) != "" {
			host = parsedHost
		}
	}
	for _, domain := range domains {
		d := strings.ToLower(strings.TrimSpace(domain))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}
