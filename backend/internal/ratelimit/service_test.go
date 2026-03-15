package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAllowMemoryFallback(t *testing.T) {
	svc := New(Config{
		Enabled:        true,
		Env:            "development",
		DevFallbackMem: true,
	})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		decision, err := svc.Allow(ctx, "test.rule", "user:1", 3, time.Minute)
		if err != nil {
			t.Fatalf("allow should not error: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	decision, err := svc.Allow(ctx, "test.rule", "user:1", 3, time.Minute)
	if err != nil {
		t.Fatalf("allow should not error on overflow: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("4th request should be blocked")
	}
	if decision.RetryAfter <= 0 {
		t.Fatalf("expected positive retry after")
	}
}

func TestClaimOnceMemoryFallback(t *testing.T) {
	svc := New(Config{
		Enabled:        true,
		Env:            "development",
		DevFallbackMem: true,
	})
	ctx := context.Background()

	ok, err := svc.ClaimOnce(ctx, "once.rule", "k1", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("claim once should not error: %v", err)
	}
	if !ok {
		t.Fatalf("first claim should succeed")
	}

	ok, err = svc.ClaimOnce(ctx, "once.rule", "k1", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("second claim should not error: %v", err)
	}
	if ok {
		t.Fatalf("second claim should fail within ttl")
	}

	time.Sleep(60 * time.Millisecond)
	ok, err = svc.ClaimOnce(ctx, "once.rule", "k1", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("third claim should not error after ttl: %v", err)
	}
	if !ok {
		t.Fatalf("third claim should succeed after ttl")
	}
}

func TestFailClosedProductionWhenNoBackend(t *testing.T) {
	svc := New(Config{
		Enabled:        true,
		Env:            "production",
		FailClosedProd: true,
		DevFallbackMem: false,
	})
	ctx := context.Background()

	_, err := svc.Allow(ctx, "test.rule", "user:1", 1, time.Minute)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}

	_, err = svc.ClaimOnce(ctx, "once.rule", "k1", time.Minute)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable for claim once, got %v", err)
	}
}
