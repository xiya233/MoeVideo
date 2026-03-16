package app

import (
	"database/sql"

	"moevideo/backend/internal/auth"
	"moevideo/backend/internal/config"
	"moevideo/backend/internal/ratelimit"
	"moevideo/backend/internal/storage"
)

type ImportControl interface {
	CancelJob(jobID string) bool
}

type App struct {
	Config    config.Config
	DB        *sql.DB
	JWT       *auth.Manager
	Storage   *storage.Service
	RateLim   *ratelimit.Service
	ImportCtl ImportControl
}
