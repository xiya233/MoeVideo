package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env              string
	HTTPAddr         string
	DBPath           string
	JWTSecret        string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	StorageDriver    string
	LocalStorageDir  string
	PublicBaseURL    string
	MaxUploadBytes   int64
	UploadURLExpires time.Duration
	FFmpegBin        string
	FFprobeBin       string
	TranscodePoll    time.Duration
	TranscodeMaxTry  int

	S3Bucket          string
	S3Region          string
	S3Endpoint        string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3SessionToken    string
	S3ForcePathStyle  bool
	S3PublicBaseURL   string
}

func Load() (Config, error) {
	cfg := Config{
		Env:               getEnv("APP_ENV", "development"),
		HTTPAddr:          getEnv("HTTP_ADDR", ":8080"),
		DBPath:            getEnv("DB_PATH", "./data/moevideo.db"),
		JWTSecret:         getEnv("JWT_SECRET", "change-me-in-production"),
		StorageDriver:     strings.ToLower(getEnv("STORAGE_DRIVER", "local")),
		LocalStorageDir:   getEnv("LOCAL_STORAGE_DIR", "./storage/local"),
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
	}

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
