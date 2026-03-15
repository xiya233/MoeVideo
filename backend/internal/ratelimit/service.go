package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrUnavailable = errors.New("rate limit backend unavailable")
)

type Config struct {
	Enabled        bool
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	Prefix         string
	Env            string
	FailClosedProd bool
	DevFallbackMem bool
}

type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetAt    time.Time
	RetryAfter time.Duration
}

type Service struct {
	enabled        bool
	env            string
	failClosedProd bool
	fallback       bool
	prefix         string
	redis          *redis.Client
	mem            *memoryStore
}

var allowLua = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return current
`)

func New(cfg Config) *Service {
	prefix := strings.TrimSpace(cfg.Prefix)
	if prefix == "" {
		prefix = "moevideo"
	}
	s := &Service{
		enabled:        cfg.Enabled,
		env:            strings.ToLower(strings.TrimSpace(cfg.Env)),
		failClosedProd: cfg.FailClosedProd,
		fallback:       cfg.DevFallbackMem,
		prefix:         prefix,
		mem:            newMemoryStore(),
	}
	if strings.TrimSpace(cfg.RedisAddr) != "" {
		s.redis = redis.NewClient(&redis.Options{
			Addr:     strings.TrimSpace(cfg.RedisAddr),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}
	return s
}

func (s *Service) Close() error {
	if s == nil || s.redis == nil {
		return nil
	}
	return s.redis.Close()
}

func (s *Service) Enabled() bool {
	return s != nil && s.enabled
}

func (s *Service) Allow(ctx context.Context, ruleID, key string, limit int, window time.Duration) (Decision, error) {
	if s == nil || !s.enabled {
		return allowAll(limit), nil
	}
	if limit <= 0 {
		return Decision{}, fmt.Errorf("invalid limit: %d", limit)
	}
	if window <= 0 {
		return Decision{}, fmt.Errorf("invalid window: %s", window)
	}

	now := time.Now().UTC()
	resetAt := windowResetAt(now, window)
	ttl := time.Until(resetAt)
	if ttl <= 0 {
		ttl = window
	}
	slot := now.UnixNano() / int64(window)
	hash := hashKey(key)
	redisKey := fmt.Sprintf("%s:rl:%s:%s:%d", s.prefix, ruleID, hash, slot)

	count, err := s.allowCount(ctx, redisKey, ttl, resetAt)
	if err != nil {
		return Decision{}, err
	}
	remaining := limit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	decision := Decision{
		Allowed:   count <= int64(limit),
		Limit:     limit,
		Remaining: remaining,
		ResetAt:   resetAt,
	}
	if !decision.Allowed {
		decision.RetryAfter = time.Until(resetAt)
		if decision.RetryAfter < time.Second {
			decision.RetryAfter = time.Second
		}
	}
	return decision, nil
}

func (s *Service) ClaimOnce(ctx context.Context, name, key string, ttl time.Duration) (bool, error) {
	if s == nil || !s.enabled {
		return true, nil
	}
	if ttl <= 0 {
		return false, fmt.Errorf("invalid ttl: %s", ttl)
	}
	redisKey := fmt.Sprintf("%s:once:%s:%s", s.prefix, name, hashKey(key))
	if s.redis != nil {
		ok, err := s.redis.SetNX(ctx, redisKey, "1", ttl).Result()
		if err == nil {
			return ok, nil
		}
		if disallowed := s.handleBackendErr(err); disallowed != nil {
			return false, disallowed
		}
	}
	if s.fallbackAllowed() {
		return s.mem.claimOnce(redisKey, ttl), nil
	}
	return false, ErrUnavailable
}

func (s *Service) allowCount(ctx context.Context, redisKey string, ttl time.Duration, resetAt time.Time) (int64, error) {
	if s.redis != nil {
		ms := ttl.Milliseconds()
		if ms <= 0 {
			ms = 1
		}
		res, err := allowLua.Run(ctx, s.redis, []string{redisKey}, ms).Result()
		if err == nil {
			count, convErr := redisInt64(res)
			if convErr != nil {
				return 0, convErr
			}
			return count, nil
		}
		if disallowed := s.handleBackendErr(err); disallowed != nil {
			return 0, disallowed
		}
	}
	if s.fallbackAllowed() {
		return s.mem.allow(redisKey, resetAt), nil
	}
	return 0, ErrUnavailable
}

func (s *Service) fallbackAllowed() bool {
	if !s.fallback {
		return false
	}
	return s.env != "production"
}

func (s *Service) handleBackendErr(err error) error {
	if err == nil {
		return nil
	}
	if s.env == "production" && s.failClosedProd {
		return ErrUnavailable
	}
	return nil
}

func allowAll(limit int) Decision {
	if limit <= 0 {
		limit = 1
	}
	return Decision{
		Allowed:    true,
		Limit:      limit,
		Remaining:  limit,
		ResetAt:    time.Now().UTC().Add(time.Minute),
		RetryAfter: 0,
	}
}

func windowResetAt(now time.Time, window time.Duration) time.Time {
	base := now.UnixNano() / int64(window)
	return time.Unix(0, (base+1)*int64(window)).UTC()
}

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func redisInt64(v interface{}) (int64, error) {
	switch t := v.(type) {
	case int64:
		return t, nil
	case int:
		return int64(t), nil
	case string:
		return strconv.ParseInt(t, 10, 64)
	default:
		return 0, fmt.Errorf("unexpected redis value type %T", v)
	}
}
