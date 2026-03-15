package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"

	"moevideo/backend/internal/middleware"
	"moevideo/backend/internal/pagination"
	"moevideo/backend/internal/response"
	"moevideo/backend/internal/util"
)

const (
	defaultDanmakuLoadLimit = 500
	maxDanmakuLoadLimit     = 5000
)

var danmakuColorPattern = regexp.MustCompile(`^#?[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$`)

type createDanmakuRequest struct {
	Content string  `json:"content"`
	TimeSec float64 `json:"time_sec"`
	Mode    int     `json:"mode"`
	Color   string  `json:"color"`
}

type danmakuCursor struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

type videoVisibility struct {
	UploaderID string
	Status     string
	Visibility string
	Duration   int64
}

type danmakuItem struct {
	ID        string  `json:"id"`
	VideoID   string  `json:"video_id"`
	UserID    string  `json:"user_id"`
	Content   string  `json:"content"`
	TimeSec   float64 `json:"time_sec"`
	Mode      int     `json:"mode"`
	Color     string  `json:"color"`
	CreatedAt string  `json:"created_at"`
}

type danmakuSubscriber struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *danmakuSubscriber) writeText(payload []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return s.conn.WriteMessage(websocket.TextMessage, payload) == nil
}

func (s *danmakuSubscriber) writeControl(messageType int, data []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	return s.conn.WriteControl(messageType, data, deadline) == nil
}

func (s *danmakuSubscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.Close()
}

type danmakuHub struct {
	mu    sync.RWMutex
	rooms map[string]map[*danmakuSubscriber]struct{}
}

func newDanmakuHub() *danmakuHub {
	return &danmakuHub{
		rooms: make(map[string]map[*danmakuSubscriber]struct{}),
	}
}

func (h *danmakuHub) add(videoID string, subscriber *danmakuSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.rooms[videoID]
	if room == nil {
		room = make(map[*danmakuSubscriber]struct{})
		h.rooms[videoID] = room
	}
	room[subscriber] = struct{}{}
}

func (h *danmakuHub) remove(videoID string, subscriber *danmakuSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.rooms[videoID]
	if room == nil {
		return
	}
	delete(room, subscriber)
	if len(room) == 0 {
		delete(h.rooms, videoID)
	}
}

func (h *danmakuHub) broadcast(videoID string, payload []byte) {
	h.mu.RLock()
	room := h.rooms[videoID]
	if len(room) == 0 {
		h.mu.RUnlock()
		return
	}
	subscribers := make([]*danmakuSubscriber, 0, len(room))
	for subscriber := range room {
		subscribers = append(subscribers, subscriber)
	}
	h.mu.RUnlock()

	for _, subscriber := range subscribers {
		if subscriber.writeText(payload) {
			continue
		}
		h.remove(videoID, subscriber)
		subscriber.close()
	}
}

func (h *Handler) ListVideoDanmaku(c *fiber.Ctx) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}

	viewerID := currentUserID(c)
	visible, err := h.loadVideoVisibility(c.UserContext(), videoID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to load video")
	}
	if !canReadVideo(visible, viewerID) {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}

	limit := parseDanmakuLoadLimit(c.Query("limit"))
	rows, err := h.app.DB.QueryContext(
		c.UserContext(),
		`SELECT id, video_id, user_id, content, time_sec, mode, color, created_at
		 FROM video_danmaku
		 WHERE video_id = ? AND status = 'active'
		 ORDER BY time_sec ASC, created_at ASC, id ASC
		 LIMIT ?`,
		videoID,
		limit,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list danmaku")
	}
	defer rows.Close()

	items, err := scanDanmakuItems(rows)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to parse danmaku")
	}
	return response.OK(c, fiber.Map{"items": items})
}

func (h *Handler) ListVideoDanmakuTimeline(c *fiber.Ctx) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}

	viewerID := currentUserID(c)
	visible, err := h.loadVideoVisibility(c.UserContext(), videoID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to load video")
	}
	if !canReadVideo(visible, viewerID) {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}

	limit := pagination.ClampLimit(c.Query("limit"), 30, maxLimit)
	cursor := strings.TrimSpace(c.Query("cursor"))

	query := `
SELECT id, video_id, user_id, content, time_sec, mode, color, created_at
FROM video_danmaku
WHERE video_id = ? AND status = 'active'
`
	args := []interface{}{videoID}

	var cur danmakuCursor
	if cursor != "" {
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, cur.CreatedAt, cur.CreatedAt, cur.ID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list danmaku")
	}
	defer rows.Close()

	items, err := scanDanmakuItems(rows)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to parse danmaku list")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		nextCursor, err = pagination.Encode(danmakuCursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
	}

	return response.OK(c, fiber.Map{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) CreateVideoDanmaku(c *fiber.Ctx) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}
	viewerID := currentUserID(c)
	if viewerID == "" {
		return response.Error(c, fiber.StatusUnauthorized, "login required")
	}

	visible, err := h.loadVideoVisibility(c.UserContext(), videoID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to load video")
	}
	if !canReadVideo(visible, viewerID) {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}

	var req createDanmakuRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	content := strings.TrimSpace(req.Content)
	contentLength := utf8.RuneCountInString(content)
	if contentLength < 1 || contentLength > 200 {
		return response.Error(c, fiber.StatusBadRequest, "content length must be 1-200")
	}
	if req.TimeSec < 0 {
		return response.Error(c, fiber.StatusBadRequest, "time_sec must be >= 0")
	}
	if visible.Duration > 0 && req.TimeSec > float64(visible.Duration) {
		return response.Error(c, fiber.StatusBadRequest, "time_sec exceeds video duration")
	}
	if req.Mode < 0 || req.Mode > 2 {
		return response.Error(c, fiber.StatusBadRequest, "mode must be 0/1/2")
	}

	color, err := normalizeDanmakuColor(req.Color)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "color must be hex like #FFFFFF")
	}
	roundedTime := strconv.FormatInt(int64(math.Round(req.TimeSec)), 10)
	dedupeKey := strings.Join([]string{
		viewerID,
		videoID,
		roundedTime,
		util.SHA256Hex(content),
	}, ":")
	ok, err := h.claimOnce(c, "danmaku-content", dedupeKey, 10*time.Second)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "anti-spam unavailable")
	}
	if !ok {
		return h.respondRateLimited(c, "interaction.danmaku.duplicate", 10*time.Second, "duplicate danmaku submitted too fast")
	}

	item := danmakuItem{
		ID:        newID(),
		VideoID:   videoID,
		UserID:    viewerID,
		Content:   content,
		TimeSec:   req.TimeSec,
		Mode:      req.Mode,
		Color:     color,
		CreatedAt: nowString(),
	}

	if _, err := h.app.DB.ExecContext(
		c.UserContext(),
		`INSERT INTO video_danmaku (id, video_id, user_id, content, time_sec, mode, color, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?)`,
		item.ID,
		item.VideoID,
		item.UserID,
		item.Content,
		item.TimeSec,
		item.Mode,
		item.Color,
		item.CreatedAt,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create danmaku")
	}

	payload, err := json.Marshal(fiber.Map{
		"event": "danmaku_created",
		"data":  item,
	})
	if err == nil {
		h.danmakuHub.broadcast(videoID, payload)
	}

	return response.Created(c, fiber.Map{"item": item})
}

func (h *Handler) SubscribeVideoDanmakuWS(c *websocket.Conn) {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		_ = c.WriteJSON(fiber.Map{"event": "error", "message": "videoId is required"})
		_ = c.Close()
		return
	}

	viewerID := h.resolveWSViewerID(c)
	visible, err := h.loadVideoVisibility(context.Background(), videoID)
	if err != nil || !canReadVideo(visible, viewerID) {
		_ = c.WriteJSON(fiber.Map{"event": "error", "message": "video not found"})
		_ = c.Close()
		return
	}

	subscriber := &danmakuSubscriber{conn: c}
	h.danmakuHub.add(videoID, subscriber)
	defer func() {
		h.danmakuHub.remove(videoID, subscriber)
		subscriber.close()
	}()

	c.SetReadLimit(2048)
	_ = c.SetReadDeadline(time.Now().Add(75 * time.Second))
	c.SetPongHandler(func(string) error {
		return c.SetReadDeadline(time.Now().Add(75 * time.Second))
	})

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if subscriber.writeControl(websocket.PingMessage, []byte("ping")) {
					continue
				}
				subscriber.close()
				return
			}
		}
	}()

	for {
		if _, _, err := c.ReadMessage(); err != nil {
			close(done)
			return
		}
	}
}

func (h *Handler) resolveWSViewerID(c *websocket.Conn) string {
	accessToken := strings.TrimSpace(cookieValueFromHeader(c.Headers("Cookie"), middleware.AccessTokenCookieName))
	if accessToken == "" {
		accessToken = strings.TrimSpace(c.Query("access_token"))
	}
	if accessToken == "" {
		accessToken = extractBearerToken(c.Headers("Authorization"))
	}
	if accessToken == "" {
		return ""
	}
	claims, err := h.app.JWT.ParseAccessToken(accessToken)
	if err != nil {
		return ""
	}
	return claims.UserID
}

func cookieValueFromHeader(raw, key string) string {
	header := http.Header{}
	header.Set("Cookie", raw)
	req := http.Request{Header: header}
	cookie, err := req.Cookie(key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func (h *Handler) loadVideoVisibility(ctx context.Context, videoID string) (videoVisibility, error) {
	var visible videoVisibility
	err := h.app.DB.QueryRowContext(
		ctx,
		`SELECT uploader_id, status, COALESCE(visibility, 'public'), COALESCE(duration_sec, 0)
		 FROM videos
		 WHERE id = ?
		 LIMIT 1`,
		videoID,
	).Scan(&visible.UploaderID, &visible.Status, &visible.Visibility, &visible.Duration)
	if err != nil {
		return videoVisibility{}, err
	}
	return visible, nil
}

func canReadVideo(visible videoVisibility, viewerID string) bool {
	if visible.Status == "deleted" {
		return false
	}
	isOwner := viewerID != "" && viewerID == visible.UploaderID
	if isOwner {
		return true
	}
	if visible.Status != "published" {
		return false
	}
	return visible.Visibility == "public" || visible.Visibility == "unlisted" || visible.Visibility == ""
}

func parseDanmakuLoadLimit(raw string) int {
	limit := defaultDanmakuLoadLimit
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return limit
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return limit
	}
	if parsed <= 0 {
		return limit
	}
	if parsed > maxDanmakuLoadLimit {
		return maxDanmakuLoadLimit
	}
	return parsed
}

func normalizeDanmakuColor(raw string) (string, error) {
	color := strings.TrimSpace(raw)
	if color == "" {
		return "#FFFFFF", nil
	}
	if !danmakuColorPattern.MatchString(color) {
		return "", fmt.Errorf("invalid color")
	}
	if !strings.HasPrefix(color, "#") {
		color = "#" + color
	}
	hexPart := strings.TrimPrefix(color, "#")
	if len(hexPart) == 3 {
		expanded := strings.Builder{}
		for _, ch := range hexPart {
			expanded.WriteRune(ch)
			expanded.WriteRune(ch)
		}
		hexPart = expanded.String()
	}
	return "#" + strings.ToUpper(hexPart), nil
}

func extractBearerToken(authHeader string) string {
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func scanDanmakuItems(rows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Err() error
}) ([]danmakuItem, error) {
	items := make([]danmakuItem, 0)
	for rows.Next() {
		var item danmakuItem
		if err := rows.Scan(
			&item.ID,
			&item.VideoID,
			&item.UserID,
			&item.Content,
			&item.TimeSec,
			&item.Mode,
			&item.Color,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
