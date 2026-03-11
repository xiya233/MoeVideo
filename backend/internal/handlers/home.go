package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/pagination"
	"moevideo/backend/internal/response"
)

func (h *Handler) ListCategories(c *fiber.Ctx) error {
	rows, err := h.app.DB.QueryContext(c.UserContext(), `SELECT id, slug, name, sort_order FROM categories WHERE is_active = 1 ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch categories")
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var slug, name string
		var sortOrder int64
		if err := rows.Scan(&id, &slug, &name, &sortOrder); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to scan categories")
		}
		items = append(items, map[string]interface{}{
			"id":         id,
			"slug":       slug,
			"name":       name,
			"sort_order": sortOrder,
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to read categories")
	}
	return response.OK(c, items)
}

func (h *Handler) GetHotRankings(c *fiber.Ctx) error {
	limit := pagination.ClampLimit(c.Query("limit"), 10, maxLimit)
	cards, err := h.queryVideoCards(c.UserContext(), videoQueryOptions{Limit: limit, Sort: "hot"})
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch hot rankings")
	}
	return response.OK(c, fiber.Map{"items": cards})
}

func (h *Handler) GetHome(c *fiber.Ctx) error {
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := c.Query("cursor")
	q := strings.TrimSpace(c.Query("q"))
	category := strings.TrimSpace(c.Query("category"))

	hot, err := h.queryVideoCards(c.UserContext(), videoQueryOptions{Limit: 10, Sort: "hot"})
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch home hot rankings")
	}

	featuredList, err := h.queryVideoCards(c.UserContext(), videoQueryOptions{Limit: 1, Sort: "latest"})
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch featured video")
	}
	var featured interface{} = nil
	if len(featuredList) > 0 {
		featured = featuredList[0]
	}

	cards, nextCursor, err := h.queryVideoCardsWithCursor(c.UserContext(), videoQueryOptions{
		Limit:    limit,
		Sort:     "latest",
		Category: category,
		Query:    q,
		Cursor:   cursor,
	})
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch home feed")
	}

	rows, err := h.app.DB.QueryContext(c.UserContext(), `SELECT id, slug, name, sort_order FROM categories WHERE is_active = 1 ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load categories")
	}
	defer rows.Close()
	categories := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var slug, name string
		var sortOrder int64
		if err := rows.Scan(&id, &slug, &name, &sortOrder); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse categories")
		}
		categories = append(categories, map[string]interface{}{
			"id":         id,
			"slug":       slug,
			"name":       name,
			"sort_order": sortOrder,
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to read categories")
	}

	return response.OK(c, fiber.Map{
		"featured":     featured,
		"hot_rankings": hot,
		"categories":   categories,
		"videos":       cards,
		"next_cursor":  nextCursor,
	})
}

type videoQueryOptions struct {
	Limit    int
	Sort     string
	Category string
	Query    string
	Cursor   string
}

func (h *Handler) queryVideoCards(ctx context.Context, opts videoQueryOptions) ([]map[string]interface{}, error) {
	cards, _, err := h.queryVideoCardsWithCursor(ctx, opts)
	return cards, err
}

func (h *Handler) queryVideoCardsWithCursor(ctx context.Context, opts videoQueryOptions) ([]map[string]interface{}, string, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	sort := strings.ToLower(strings.TrimSpace(opts.Sort))
	if sort == "" {
		sort = "latest"
	}

	query := `
SELECT v.id, v.title, v.duration_sec, v.views_count, v.comments_count, COALESCE(v.published_at, v.created_at),
       COALESCE(c.name, ''),
       COALESCE(cm.provider, ''), COALESCE(cm.bucket, ''), COALESCE(cm.object_key, ''),
       u.id, u.username, COALESCE(u.followers_count, 0),
       COALESCE(am.provider, ''), COALESCE(am.bucket, ''), COALESCE(am.object_key, '')
FROM videos v
JOIN users u ON u.id = v.uploader_id
LEFT JOIN categories c ON c.id = v.category_id
LEFT JOIN media_objects cm ON cm.id = v.cover_media_id
LEFT JOIN media_objects am ON am.id = u.avatar_media_id
WHERE v.status = 'published' AND v.visibility = 'public'
`

	args := make([]interface{}, 0)
	if opts.Category != "" {
		categoryFilter, catArgs := buildCategoryFilter(opts.Category)
		query += categoryFilter
		args = append(args, catArgs...)
	}
	if opts.Query != "" {
		query += ` AND (
			v.title LIKE ?
			OR v.description LIKE ?
			OR EXISTS (
				SELECT 1 FROM video_tags vt
				JOIN tags t ON t.id = vt.tag_id
				WHERE vt.video_id = v.id AND t.name LIKE ?
			)
		)`
		kw := "%" + opts.Query + "%"
		args = append(args, kw, kw, kw)
	}

	var cur listCursor
	if opts.Cursor != "" {
		if err := pagination.Decode(opts.Cursor, &cur); err != nil {
			return nil, "", fmt.Errorf("decode cursor: %w", err)
		}
		query += ` AND (COALESCE(v.published_at, v.created_at) < ? OR (COALESCE(v.published_at, v.created_at) = ? AND v.id < ?))`
		args = append(args, cur.PublishedAt, cur.PublishedAt, cur.ID)
	}

	switch sort {
	case "hot":
		query += ` ORDER BY v.hot_score DESC, v.id DESC`
	default:
		query += ` ORDER BY COALESCE(v.published_at, v.created_at) DESC, v.id DESC`
	}

	query += ` LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	cards, err := h.scanVideoCards(rows)
	if err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(cards) > limit {
		last := cards[limit-1]
		cards = cards[:limit]
		nextCursor, err = pagination.Encode(listCursor{
			PublishedAt: last["published_at"].(string),
			ID:          last["id"].(string),
		})
		if err != nil {
			return nil, "", err
		}
	}
	return cards, nextCursor, nil
}

func buildCategoryFilter(category string) (string, []interface{}) {
	if category == "" {
		return "", nil
	}
	if id, err := parseCategoryID(category); err == nil {
		return " AND v.category_id = ?", []interface{}{id}
	}
	return " AND c.slug = ?", []interface{}{category}
}

func parseCategoryID(category string) (int64, error) {
	id, err := strconv.ParseInt(category, 10, 64)
	if err != nil {
		return 0, err
	}
	return id, nil
}
