package db

import (
	"context"
	"database/sql"
	"fmt"
)

type categorySeed struct {
	Slug string
	Name string
}

var defaultCategories = []categorySeed{
	{Slug: "animation", Name: "动画"},
	{Slug: "games", Name: "游戏"},
	{Slug: "music", Name: "音乐"},
	{Slug: "dance", Name: "舞蹈"},
	{Slug: "life", Name: "生活"},
	{Slug: "technology", Name: "科技"},
	{Slug: "kichiku", Name: "鬼畜"},
}

func SeedCategories(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin seed tx: %w", err)
	}
	defer tx.Rollback()

	for idx, c := range defaultCategories {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO categories (slug, name, sort_order, is_active)
			 VALUES (?, ?, ?, 1)
			 ON CONFLICT(slug) DO UPDATE SET name=excluded.name, sort_order=excluded.sort_order`,
			c.Slug,
			c.Name,
			idx+1,
		); err != nil {
			return fmt.Errorf("seed category %s: %w", c.Slug, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed tx: %w", err)
	}
	return nil
}
