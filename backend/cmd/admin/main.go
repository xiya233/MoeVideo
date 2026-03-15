package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"moevideo/backend/internal/auth"
	"moevideo/backend/internal/db"
	"moevideo/backend/internal/util"
)

func main() {
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		log.Fatalf("load .env: %v", err)
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "bootstrap":
		if err := runBootstrap(os.Args[2:]); err != nil {
			log.Fatalf("admin bootstrap failed: %v", err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("Usage:")
	fmt.Println("  go run ./cmd/admin bootstrap --email admin@example.com --username admin --password <SECRET> [--db ./data/moevideo.db]")
}

func runBootstrap(args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	email := fs.String("email", "", "admin email")
	username := fs.String("username", "admin", "admin username")
	password := fs.String("password", "", "admin password")
	dbPath := fs.String("db", defaultDBPath(), "sqlite db path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	mail := strings.ToLower(strings.TrimSpace(*email))
	name := strings.TrimSpace(*username)
	pass := strings.TrimSpace(*password)
	if mail == "" || pass == "" {
		return fmt.Errorf("--email and --password are required")
	}
	if len(pass) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if name == "" {
		name = "admin"
	}

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		return fmt.Errorf("create db directory: %w", err)
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	hash, err := auth.HashPassword(pass)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	now := util.FormatTime(util.NowUTC())

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var userID, currentUsername string
	err = tx.QueryRow(`SELECT id, username FROM users WHERE email = ? LIMIT 1`, mail).Scan(&userID, &currentUsername)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("query user: %w", err)
	}

	created := false
	finalUsername := currentUsername
	if err == sql.ErrNoRows {
		finalUsername = nextAvailableUsername(tx, name)
		if _, err := tx.Exec(`
INSERT INTO users (id, username, email, password_hash, role, status, created_at, updated_at)
VALUES (?, ?, ?, ?, 'admin', 'active', ?, ?)`,
			uuid.NewString(),
			finalUsername,
			mail,
			hash,
			now,
			now,
		); err != nil {
			return fmt.Errorf("insert admin user: %w", err)
		}
		created = true
	} else {
		if strings.TrimSpace(name) != "" && name != currentUsername {
			candidate := nextAvailableUsername(tx, name)
			finalUsername = candidate
		}
		if _, err := tx.Exec(`
UPDATE users
SET username = ?, password_hash = ?, role = 'admin', status = 'active', updated_at = ?
WHERE id = ?`,
			finalUsername,
			hash,
			now,
			userID,
		); err != nil {
			return fmt.Errorf("update admin user: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	if created {
		log.Printf("admin user created: email=%s username=%s", mail, finalUsername)
	} else {
		log.Printf("admin user updated: email=%s username=%s", mail, finalUsername)
	}
	return nil
}

func defaultDBPath() string {
	if fromEnv := strings.TrimSpace(os.Getenv("DB_PATH")); fromEnv != "" {
		return fromEnv
	}
	return "./data/moevideo.db"
}

func nextAvailableUsername(tx *sql.Tx, base string) string {
	baseName := strings.TrimSpace(base)
	if baseName == "" {
		baseName = "admin"
	}
	candidate := baseName
	for i := 0; i < 100; i++ {
		var exists int
		err := tx.QueryRow(`SELECT 1 FROM users WHERE username = ? LIMIT 1`, candidate).Scan(&exists)
		if err == sql.ErrNoRows {
			return candidate
		}
		candidate = fmt.Sprintf("%s%d", baseName, i+1)
	}
	return fmt.Sprintf("%s-%s", baseName, uuid.NewString()[:8])
}
