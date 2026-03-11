package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/auth"
	"moevideo/backend/internal/config"
	"moevideo/backend/internal/db"
	"moevideo/backend/internal/handlers"
	"moevideo/backend/internal/response"
	"moevideo/backend/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		log.Fatalf("create db directory: %v", err)
	}
	if cfg.StorageDriver == "local" {
		if err := os.MkdirAll(cfg.LocalStorageDir, 0o755); err != nil {
			log.Fatalf("create local storage directory: %v", err)
		}
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	storageSvc, err := storage.NewService(cfg)
	if err != nil {
		log.Fatalf("init storage service: %v", err)
	}

	appContainer := &app.App{
		Config:  cfg,
		DB:      database,
		JWT:     auth.NewManager(cfg.JWTSecret),
		Storage: storageSvc,
	}

	server := fiber.New(fiber.Config{
		AppName: "MoeVideo API",
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			log.Printf("request error: %v", err)
			return response.Error(c, fiber.StatusInternalServerError, "internal server error")
		},
	})

	server.Use(recover.New())
	server.Use(cors.New())

	server.Get("/healthz", func(c *fiber.Ctx) error {
		return response.OK(c, fiber.Map{"status": "ok"})
	})

	if cfg.StorageDriver == "local" {
		server.Static("/media", cfg.LocalStorageDir)
	}

	api := server.Group("/api/v1")
	handlers.RegisterRoutes(api, appContainer)

	log.Printf("MoeVideo API listening on %s", cfg.HTTPAddr)
	if err := server.Listen(cfg.HTTPAddr); err != nil {
		log.Fatalf("fiber listen: %v", err)
	}
}
