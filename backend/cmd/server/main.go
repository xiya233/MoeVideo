package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/auth"
	"moevideo/backend/internal/config"
	"moevideo/backend/internal/db"
	"moevideo/backend/internal/handlers"
	"moevideo/backend/internal/importer"
	"moevideo/backend/internal/logging"
	"moevideo/backend/internal/middleware"
	"moevideo/backend/internal/ratelimit"
	"moevideo/backend/internal/response"
	"moevideo/backend/internal/storage"
	"moevideo/backend/internal/transcode"
)

func main() {
	if err := godotenv.Load(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load .env: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	appLogger, err := logging.New(cfg.LogLevel)
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		appLogger.Errorf("create db directory failed: %v", err)
		os.Exit(1)
	}
	if cfg.StorageDriver == "local" {
		if err := os.MkdirAll(cfg.LocalStorageDir, 0o755); err != nil {
			appLogger.Errorf("create local storage directory failed: %v", err)
			os.Exit(1)
		}
	}
	if err := os.MkdirAll(cfg.TaskTempDir, 0o755); err != nil {
		appLogger.Errorf("create task temp directory failed: %v", err)
		os.Exit(1)
	}
	appLogger.Infof("task temp directory initialized task_temp_dir=%s", cfg.TaskTempDir)

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		appLogger.Errorf("open database failed: %v", err)
		os.Exit(1)
	}
	defer database.Close()

	storageSvc, err := storage.NewService(cfg)
	if err != nil {
		appLogger.Errorf("init storage service failed: %v", err)
		os.Exit(1)
	}

	appContainer := &app.App{
		Config:  cfg,
		DB:      database,
		JWT:     auth.NewManager(cfg.JWTSecret),
		Storage: storageSvc,
		RateLim: ratelimit.New(ratelimit.Config{
			Enabled:        cfg.RateLimitEnabled,
			RedisAddr:      cfg.RateLimitRedisAddr,
			RedisPassword:  cfg.RateLimitRedisPassword,
			RedisDB:        cfg.RateLimitRedisDB,
			Prefix:         cfg.RateLimitRedisPrefix,
			Env:            cfg.Env,
			FailClosedProd: cfg.RateLimitFailClosedProd,
			DevFallbackMem: cfg.RateLimitDevFallbackMem,
		}),
	}
	defer func() {
		if err := appContainer.RateLim.Close(); err != nil {
			appLogger.Warnf("close rate limiter failed: %v", err)
		}
	}()

	maxInt := int64(^uint(0) >> 1)
	bodyLimit := 4 * 1024 * 1024
	if cfg.MaxUploadBytes > 0 {
		if cfg.MaxUploadBytes > maxInt {
			bodyLimit = int(maxInt)
		} else {
			bodyLimit = int(cfg.MaxUploadBytes)
		}
	}

	server := fiber.New(fiber.Config{
		AppName:   "MoeVideo API",
		BodyLimit: bodyLimit,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if errors.Is(err, fiber.ErrRequestEntityTooLarge) {
				return response.Error(c, fiber.StatusRequestEntityTooLarge, "request entity too large")
			}
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				return response.Error(c, fiberErr.Code, fiberErr.Message)
			}
			appLogger.Errorf("request error: %v", err)
			return response.Error(c, fiber.StatusInternalServerError, "internal server error")
		},
	})

	server.Use(recover.New())
	allowOrigins := strings.Join(cfg.CORSAllowedOrigins, ",")
	server.Use(cors.New(cors.Config{
		AllowOrigins:     allowOrigins,
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS,HEAD",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization,Range",
		ExposeHeaders:    "Content-Length,Content-Range,Accept-Ranges",
		AllowCredentials: true,
	}))

	server.Get("/healthz", func(c *fiber.Ctx) error {
		return response.OK(c, fiber.Map{"status": "ok"})
	})

	if cfg.StorageDriver == "local" {
		server.Static("/media", cfg.LocalStorageDir)
	}

	api := server.Group("/api/v1")
	api.Use(middleware.RequireSameOriginWrites(cfg))
	handlers.RegisterRoutes(api, appContainer)

	workerCtx, cancelWorker := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelWorker()
	go transcode.NewWorker(
		appContainer,
		transcode.WithLogger(appLogger.WithPrefix("module=transcode")),
		transcode.WithProgressLogInterval(cfg.TranscodeProgressLogInterval),
	).Run(workerCtx)
	go importer.NewWorker(
		appContainer,
		importer.WithLogger(appLogger.WithPrefix("module=import")),
		importer.WithProgressLogInterval(cfg.ImportProgressLogInterval),
	).Run(workerCtx)
	go func() {
		<-workerCtx.Done()
		appLogger.Infof("shutdown signal received, stopping MoeVideo API")
		if err := server.Shutdown(); err != nil {
			appLogger.Errorf("graceful shutdown failed: %v", err)
		}
	}()

	appLogger.Infof("MoeVideo API listening on %s", cfg.HTTPAddr)
	if err := server.Listen(cfg.HTTPAddr); err != nil {
		if workerCtx.Err() == nil {
			appLogger.Errorf("fiber listen failed: %v", err)
			os.Exit(1)
		}
		appLogger.Infof("fiber listen exited after shutdown signal: %v", err)
	}
	appLogger.Infof("MoeVideo API stopped")
}
