package handlers

import (
	"encoding/json"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/ratelimit"
	"moevideo/backend/internal/response"
)

type routeRateLimitRule struct {
	ID      string
	Limit   int
	Window  time.Duration
	Message string
	KeyFunc func(*fiber.Ctx) (string, error)
}

func keyByIP(c *fiber.Ctx) (string, error) {
	ip := strings.TrimSpace(c.IP())
	if ip == "" {
		ip = "unknown"
	}
	return ip, nil
}

func keyByUser(c *fiber.Ctx) (string, error) {
	uid := strings.TrimSpace(currentUserID(c))
	if uid == "" {
		uid = "anon"
	}
	return uid, nil
}

func keyByUserOrIP(c *fiber.Ctx) (string, error) {
	uid := strings.TrimSpace(currentUserID(c))
	if uid != "" {
		return "uid:" + uid, nil
	}
	ip := strings.TrimSpace(c.IP())
	if ip == "" {
		ip = "unknown"
	}
	return "ip:" + ip, nil
}

func keyByLoginAccount(c *fiber.Ctx) (string, error) {
	type payload struct {
		Account string `json:"account"`
	}
	var req payload
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return keyByIP(c)
	}
	account := strings.ToLower(strings.TrimSpace(req.Account))
	if account == "" {
		return keyByIP(c)
	}
	return account, nil
}

func keyByViewerVideo(c *fiber.Ctx) (string, error) {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		videoID = "unknown"
	}
	return clientViewerKey(c) + ":" + videoID, nil
}

func (h *Handler) rateLimit(rule routeRateLimitRule) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h.app == nil || h.app.RateLim == nil || !h.app.RateLim.Enabled() {
			return c.Next()
		}
		key, err := rule.KeyFunc(c)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid rate limit key")
		}
		decision, err := h.app.RateLim.Allow(c.UserContext(), rule.ID, key, rule.Limit, rule.Window)
		if err != nil {
			log.Printf("rate_limit rule_id=%s user_id=%s ip=%s path=%s status=backend_error retry_after=0 err=%v",
				rule.ID, currentUserID(c), c.IP(), c.Path(), err)
			return response.Error(c, fiber.StatusServiceUnavailable, "rate limit unavailable")
		}

		setRateLimitHeaders(c, decision)
		if decision.Allowed {
			return c.Next()
		}

		retryAfter := retryAfterSeconds(decision.RetryAfter)
		log.Printf("rate_limit rule_id=%s user_id=%s ip=%s path=%s status=blocked retry_after=%d",
			rule.ID, currentUserID(c), c.IP(), c.Path(), retryAfter)
		return h.respondRateLimited(c, rule.ID, decision.RetryAfter, rule.Message)
	}
}

func (h *Handler) respondRateLimited(c *fiber.Ctx, ruleID string, retryAfter time.Duration, message string) error {
	if message == "" {
		message = "too many requests"
	}
	sec := retryAfterSeconds(retryAfter)
	if sec <= 0 {
		sec = 1
	}
	c.Set("Retry-After", strconv.FormatInt(sec, 10))
	return response.JSON(c, fiber.StatusTooManyRequests, message, fiber.Map{
		"rule_id":         ruleID,
		"retry_after_sec": sec,
	})
}

func (h *Handler) claimOnce(c *fiber.Ctx, name, key string, ttl time.Duration) (bool, error) {
	if h.app == nil || h.app.RateLim == nil || !h.app.RateLim.Enabled() {
		return true, nil
	}
	return h.app.RateLim.ClaimOnce(c.UserContext(), name, key, ttl)
}

func retryAfterSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(math.Ceil(d.Seconds()))
}

func setRateLimitHeaders(c *fiber.Ctx, decision ratelimit.Decision) {
	resetUnix := decision.ResetAt.Unix()
	c.Set("X-RateLimit-Limit", strconv.Itoa(decision.Limit))
	c.Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))
	c.Set("X-RateLimit-Reset", strconv.FormatInt(resetUnix, 10))
}

var (
	rlAuthCaptchaIP = routeRateLimitRule{ID: "auth.captcha.ip", Limit: 20, Window: time.Minute, Message: "too many captcha requests", KeyFunc: keyByIP}
	rlAuthLoginIP   = routeRateLimitRule{ID: "auth.login.ip", Limit: 10, Window: time.Minute, Message: "too many login requests", KeyFunc: keyByIP}
	rlAuthLoginAcct = routeRateLimitRule{ID: "auth.login.account", Limit: 30, Window: 10 * time.Minute, Message: "too many login attempts for account", KeyFunc: keyByLoginAccount}
	rlAuthRegIP     = routeRateLimitRule{ID: "auth.register.ip", Limit: 5, Window: 10 * time.Minute, Message: "too many registration requests", KeyFunc: keyByIP}
	rlAuthRefreshIP = routeRateLimitRule{ID: "auth.refresh.ip", Limit: 30, Window: time.Minute, Message: "too many refresh requests", KeyFunc: keyByIP}

	rlImportInspectUser = routeRateLimitRule{ID: "import.inspect.user", Limit: 3, Window: time.Minute, Message: "too many inspect requests", KeyFunc: keyByUser}
	rlImportInspectIP   = routeRateLimitRule{ID: "import.inspect.ip", Limit: 10, Window: time.Minute, Message: "too many inspect requests", KeyFunc: keyByIP}
	rlImportStartT      = routeRateLimitRule{ID: "import.start.torrent.user", Limit: 6, Window: time.Minute, Message: "too many import start requests", KeyFunc: keyByUser}
	rlImportStartURL    = routeRateLimitRule{ID: "import.start.url.user", Limit: 6, Window: time.Minute, Message: "too many import start requests", KeyFunc: keyByUser}

	rlCommentCreate = routeRateLimitRule{ID: "interaction.comment.create.user", Limit: 20, Window: 10 * time.Minute, Message: "too many comments", KeyFunc: keyByUser}
	rlCommentBurst  = routeRateLimitRule{ID: "interaction.comment.burst.user", Limit: 3, Window: 10 * time.Second, Message: "commenting too fast", KeyFunc: keyByUser}
	rlDanmakuCreate = routeRateLimitRule{ID: "interaction.danmaku.create.user", Limit: 120, Window: time.Minute, Message: "too many danmaku messages", KeyFunc: keyByUser}
	rlDanmakuBurst  = routeRateLimitRule{ID: "interaction.danmaku.burst.user", Limit: 5, Window: 10 * time.Second, Message: "sending danmaku too fast", KeyFunc: keyByUser}

	rlInteractionUser = routeRateLimitRule{ID: "interaction.toggle.user", Limit: 60, Window: time.Minute, Message: "too many interaction requests", KeyFunc: keyByUser}
	rlShareRate       = routeRateLimitRule{ID: "interaction.share.user_or_ip", Limit: 30, Window: time.Minute, Message: "too many share requests", KeyFunc: keyByUserOrIP}
	rlViewRate        = routeRateLimitRule{ID: "interaction.view.viewer_video", Limit: 60, Window: time.Minute, Message: "too many view requests", KeyFunc: keyByViewerVideo}
	rlProgressRate    = routeRateLimitRule{ID: "interaction.progress.user", Limit: 120, Window: time.Minute, Message: "too many progress updates", KeyFunc: keyByUser}
)
