package handlers

import (
	"testing"
	"time"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/config"
)

func newInspectTestHandler(secret string) *Handler {
	return &Handler{
		app: &app.App{
			Config: config.Config{
				JWTSecret: secret,
			},
		},
	}
}

func TestURLInspectTokenRoundTrip(t *testing.T) {
	t.Parallel()

	h := newInspectTestHandler("unit-test-secret")
	original := urlInspectTokenPayload{
		UserID:     "u1",
		SourceURL:  "https://example.com/watch/123",
		Candidates: []string{"https://cdn.example.com/master.m3u8"},
		ExpiresAt:  time.Now().Add(5 * time.Minute).Unix(),
	}

	token, err := h.signURLInspectToken(original)
	if err != nil {
		t.Fatalf("signURLInspectToken returned error: %v", err)
	}

	parsed, err := h.verifyURLInspectToken(token, "u1", "https://example.com/watch/123")
	if err != nil {
		t.Fatalf("verifyURLInspectToken returned error: %v", err)
	}

	if parsed.UserID != original.UserID || parsed.SourceURL != original.SourceURL {
		t.Fatalf("parsed payload mismatch: %+v", parsed)
	}
	if len(parsed.Candidates) != 1 || parsed.Candidates[0] != original.Candidates[0] {
		t.Fatalf("parsed candidates mismatch: %+v", parsed.Candidates)
	}
}

func TestURLInspectTokenExpired(t *testing.T) {
	t.Parallel()

	h := newInspectTestHandler("unit-test-secret")
	token, err := h.signURLInspectToken(urlInspectTokenPayload{
		UserID:     "u1",
		SourceURL:  "https://example.com/watch/123",
		Candidates: []string{"https://cdn.example.com/master.m3u8"},
		ExpiresAt:  time.Now().Add(-1 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("signURLInspectToken returned error: %v", err)
	}

	if _, err := h.verifyURLInspectToken(token, "u1", "https://example.com/watch/123"); err == nil {
		t.Fatalf("expected verifyURLInspectToken to fail for expired token")
	}
}

func TestURLInspectTokenSourceMismatch(t *testing.T) {
	t.Parallel()

	h := newInspectTestHandler("unit-test-secret")
	token, err := h.signURLInspectToken(urlInspectTokenPayload{
		UserID:     "u1",
		SourceURL:  "https://example.com/watch/abc",
		Candidates: []string{"https://cdn.example.com/master.m3u8"},
		ExpiresAt:  time.Now().Add(5 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("signURLInspectToken returned error: %v", err)
	}

	if _, err := h.verifyURLInspectToken(token, "u1", "https://example.com/watch/xyz"); err == nil {
		t.Fatalf("expected verifyURLInspectToken to fail for source url mismatch")
	}
}
