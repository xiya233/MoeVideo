package importer

import (
	"errors"
	"testing"
)

func TestSplitCommandArgs(t *testing.T) {
	args, err := splitCommandArgs(`bun backend/scripts/page_manifest_resolver.mjs --flag "hello world" --name='demo'`)
	if err != nil {
		t.Fatalf("splitCommandArgs returned error: %v", err)
	}
	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d (%v)", len(args), args)
	}
	if args[0] != "bun" || args[1] != "backend/scripts/page_manifest_resolver.mjs" {
		t.Fatalf("unexpected command split result: %v", args)
	}
}

func TestParsePageManifestResolverOutput(t *testing.T) {
	raw := []byte(`{"final_url":"https://example.com/watch/1","title":"demo","candidates":["https://cdn.example.com/master.m3u8"],"reason":"","challenge":false}`)
	out, err := parsePageManifestResolverOutput(raw)
	if err != nil {
		t.Fatalf("parsePageManifestResolverOutput returned error: %v", err)
	}
	if out.FinalURL != "https://example.com/watch/1" {
		t.Fatalf("unexpected final_url: %s", out.FinalURL)
	}
	if len(out.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(out.Candidates))
	}
}

func TestNormalizeResolverCandidates(t *testing.T) {
	result := pageManifestResolverOutput{
		FinalURL: "https://example.com/watch/1",
		Candidates: []string{
			"/hls/master.m3u8",
			"https://cdn.example.com/video.mp4",
			"https://cdn.example.com/video.mp4",
			"/assets/logo.png",
		},
	}
	items := normalizeResolverCandidates("https://example.com/watch/1", result, 10)
	if len(items) != 2 {
		t.Fatalf("expected 2 media candidates, got %d (%v)", len(items), items)
	}
	if items[0] != "https://example.com/hls/master.m3u8" {
		t.Fatalf("unexpected first candidate: %s", items[0])
	}
}

func TestUnsupportedURLErrorDetection(t *testing.T) {
	baseErr := unsupportedURLMetadataError{message: "yt-dlp metadata failed: unsupported url"}
	if !isUnsupportedURLMetadataError(baseErr) {
		t.Fatalf("expected unsupported error to be detected")
	}
	if isUnsupportedURLMetadataError(errors.New("other")) {
		t.Fatalf("unexpected unsupported detection for generic error")
	}
}
