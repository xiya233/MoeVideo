package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	defaultPageResolverCmd           = "bun scripts/page_manifest_resolver.mjs"
	defaultPageResolverTimeout       = 25 * time.Second
	defaultPageResolverMaxCandidates = 20
)

type unsupportedURLMetadataError struct {
	message string
}

func (e unsupportedURLMetadataError) Error() string {
	return e.message
}

func isUnsupportedURLMetadataError(err error) bool {
	var unsupported unsupportedURLMetadataError
	return errors.As(err, &unsupported)
}

type pageManifestResolverOutput struct {
	FinalURL      string            `json:"final_url"`
	Title         string            `json:"title"`
	Candidates    []string          `json:"candidates"`
	PageUserAgent string            `json:"page_user_agent"`
	PageReferer   string            `json:"page_referer"`
	PageOrigin    string            `json:"page_origin"`
	PageHeaders   map[string]string `json:"page_headers"`
	Reason        string            `json:"reason"`
}

func (w *Worker) resolvePageManifestCandidates(ctx context.Context, sourceURL string) ([]string, pageManifestResolverOutput, error) {
	if !w.app.Config.ImportPageResolverEnabled {
		return nil, pageManifestResolverOutput{}, fmt.Errorf("page resolver is disabled")
	}
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return nil, pageManifestResolverOutput{}, fmt.Errorf("source_url is required")
	}

	timeout := w.app.Config.ImportPageResolverTimeout
	if timeout <= 0 {
		timeout = defaultPageResolverTimeout
	}
	maxCandidates := w.app.Config.ImportPageResolverMax
	if maxCandidates <= 0 {
		maxCandidates = defaultPageResolverMaxCandidates
	}

	cmdLine := strings.TrimSpace(w.app.Config.ImportPageResolverCmd)
	if cmdLine == "" {
		cmdLine = defaultPageResolverCmd
	}
	cmdParts, err := splitCommandArgs(cmdLine)
	if err != nil {
		return nil, pageManifestResolverOutput{}, fmt.Errorf("invalid IMPORT_PAGE_RESOLVER_CMD: %w", err)
	}
	if len(cmdParts) == 0 {
		return nil, pageManifestResolverOutput{}, fmt.Errorf("IMPORT_PAGE_RESOLVER_CMD is empty")
	}

	args := append([]string{}, cmdParts[1:]...)
	args = append(args,
		"--url", sourceURL,
		"--timeout-ms", strconv.FormatInt(timeout.Milliseconds(), 10),
		"--max-candidates", strconv.Itoa(maxCandidates),
	)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, cmdParts[0], args...)
	w.logger.Infof(
		"page resolver start source_url=%s cmd=%s timeout=%s max_candidates=%d",
		sourceURL,
		formatCommand(cmdParts[0], args),
		timeout,
		maxCandidates,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, pageManifestResolverOutput{}, fmt.Errorf("page resolver timeout")
		}
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = truncateOutput(stdout.Bytes(), 400)
		} else {
			errText = truncateOutput([]byte(errText), 400)
		}
		w.logger.Debugf(
			"page resolver failed source_url=%s stdout=%s stderr=%s",
			sourceURL,
			truncateOutput(stdout.Bytes(), 1200),
			truncateOutput(stderr.Bytes(), 1200),
		)
		return nil, pageManifestResolverOutput{}, fmt.Errorf("page resolver failed: %w: %s", err, errText)
	}
	w.logger.Debugf(
		"page resolver raw output source_url=%s stdout=%s stderr=%s",
		sourceURL,
		truncateOutput(stdout.Bytes(), 1200),
		truncateOutput(stderr.Bytes(), 1200),
	)

	out, err := parsePageManifestResolverOutput(stdout.Bytes())
	if err != nil {
		return nil, pageManifestResolverOutput{}, fmt.Errorf("parse page resolver output: %w", err)
	}

	candidates := normalizeResolverCandidates(sourceURL, out, maxCandidates)
	w.logger.Infof(
		"page resolver done source_url=%s final_url=%s candidate_count=%d reason=%s",
		sourceURL,
		strings.TrimSpace(out.FinalURL),
		len(candidates),
		strings.TrimSpace(out.Reason),
	)
	if len(candidates) == 0 {
		reason := strings.TrimSpace(out.Reason)
		if reason == "" {
			reason = "no media candidates found"
		}
		return nil, out, errors.New(reason)
	}
	return candidates, out, nil
}

func parsePageManifestResolverOutput(raw []byte) (pageManifestResolverOutput, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return pageManifestResolverOutput{}, fmt.Errorf("empty output")
	}

	var out pageManifestResolverOutput
	if err := json.Unmarshal([]byte(text), &out); err == nil {
		return out, nil
	}

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return pageManifestResolverOutput{}, fmt.Errorf("invalid json output")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return pageManifestResolverOutput{}, fmt.Errorf("invalid json output")
	}
	return out, nil
}

func normalizeResolverCandidates(sourceURL string, result pageManifestResolverOutput, maxCandidates int) []string {
	baseURL := strings.TrimSpace(result.FinalURL)
	if baseURL == "" {
		baseURL = sourceURL
	}

	candidates := make([]string, 0, len(result.Candidates))
	seen := make(map[string]struct{}, len(result.Candidates))
	for _, rawCandidate := range result.Candidates {
		candidate := normalizeCandidateURL(baseURL, rawCandidate)
		if candidate == "" {
			continue
		}
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		if !looksLikeMediaCandidateURL(candidate) {
			continue
		}
		seen[key] = struct{}{}
		candidates = append(candidates, candidate)
		if maxCandidates > 0 && len(candidates) >= maxCandidates {
			break
		}
	}
	return candidates
}

func normalizeCandidateURL(baseURL, rawCandidate string) string {
	candidate := strings.TrimSpace(rawCandidate)
	if candidate == "" {
		return ""
	}
	candidate = strings.Trim(candidate, `"'`)
	if candidate == "" {
		return ""
	}
	if strings.HasPrefix(candidate, "data:") || strings.HasPrefix(candidate, "blob:") || strings.HasPrefix(candidate, "javascript:") {
		return ""
	}

	if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
		return candidate
	}
	if strings.HasPrefix(candidate, "//") {
		base, err := url.Parse(baseURL)
		if err != nil || base.Scheme == "" {
			return "https:" + candidate
		}
		return base.Scheme + ":" + candidate
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	relative, err := url.Parse(candidate)
	if err != nil {
		return ""
	}
	return base.ResolveReference(relative).String()
}

func looksLikeMediaCandidateURL(candidate string) bool {
	lower := strings.ToLower(strings.TrimSpace(candidate))
	if lower == "" {
		return false
	}
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	switch {
	case strings.Contains(lower, ".m3u8"):
		return true
	case strings.Contains(lower, ".mp4"):
		return true
	case strings.Contains(lower, ".webm"):
		return true
	case strings.Contains(lower, ".mov"):
		return true
	case strings.Contains(lower, ".mkv"):
		return true
	case strings.Contains(lower, ".ts"):
		return true
	default:
		return false
	}
}

func splitCommandArgs(raw string) ([]string, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return []string{}, nil
	}
	args := make([]string, 0, 8)
	var current strings.Builder
	var quote rune
	escaped := false

	for _, r := range text {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			continue
		}

		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		if unicode.IsSpace(r) {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}

	if escaped || quote != 0 {
		return nil, fmt.Errorf("invalid command string, quote or escape not closed")
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args, nil
}
