package handlers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	ytdlpModeSafe     = "safe"
	ytdlpModeAdvanced = "advanced"
)

type ytdlpSafeSettings struct {
	Format        string            `json:"format,omitempty"`
	ExtractorArgs string            `json:"extractor_args,omitempty"`
	UserAgent     string            `json:"user_agent,omitempty"`
	Referer       string            `json:"referer,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	SocketTimeout int64             `json:"socket_timeout,omitempty"`
}

var blockedYTDLPArgs = map[string]struct{}{
	"--exec":                 {},
	"--exec-before-download": {},
	"-o":                     {},
	"--output":               {},
	"-p":                     {},
	"--paths":                {},
	"--config-locations":     {},
	"--batch-file":           {},
}

var blockedYTDLPArgPrefixes = []string{
	"--exec=",
	"--exec-before-download=",
	"--output=",
	"--paths=",
	"--config-locations=",
	"--batch-file=",
}

func normalizeYTDLPMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return ytdlpModeSafe, nil
	}
	if mode != ytdlpModeSafe && mode != ytdlpModeAdvanced {
		return "", fmt.Errorf("ytdlp_param_mode must be safe or advanced")
	}
	return mode, nil
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
		return nil, fmt.Errorf("invalid arg string, quote or escape not closed")
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args, nil
}

func findBlockedYTDLPArg(args []string) (string, bool) {
	for _, token := range args {
		normalized := strings.ToLower(strings.TrimSpace(token))
		if normalized == "" {
			continue
		}
		if _, ok := blockedYTDLPArgs[normalized]; ok {
			return token, true
		}
		for _, prefix := range blockedYTDLPArgPrefixes {
			if strings.HasPrefix(normalized, prefix) {
				return token, true
			}
		}
		if strings.HasPrefix(normalized, "-o") && len(normalized) > 2 {
			return token, true
		}
		if strings.HasPrefix(normalized, "-p") && len(normalized) > 2 {
			return token, true
		}
	}
	return "", false
}

func validateYTDLPArgTokens(args []string) error {
	if blocked, ok := findBlockedYTDLPArg(args); ok {
		return fmt.Errorf("blocked yt-dlp arg: %s", blocked)
	}
	for _, token := range args {
		if strings.TrimSpace(token) == "" {
			continue
		}
		if len(token) > 2048 {
			return fmt.Errorf("yt-dlp arg is too long")
		}
	}
	return nil
}

func normalizeYTDLPSafeSettings(in ytdlpSafeSettings) (ytdlpSafeSettings, error) {
	out := ytdlpSafeSettings{
		Format:        strings.TrimSpace(in.Format),
		ExtractorArgs: strings.TrimSpace(in.ExtractorArgs),
		UserAgent:     strings.TrimSpace(in.UserAgent),
		Referer:       strings.TrimSpace(in.Referer),
		SocketTimeout: in.SocketTimeout,
	}

	if len(out.Format) > 256 {
		return ytdlpSafeSettings{}, fmt.Errorf("ytdlp_safe.format is too long")
	}
	if len(out.ExtractorArgs) > 2048 {
		return ytdlpSafeSettings{}, fmt.Errorf("ytdlp_safe.extractor_args is too long")
	}
	if len(out.UserAgent) > 1024 {
		return ytdlpSafeSettings{}, fmt.Errorf("ytdlp_safe.user_agent is too long")
	}
	if len(out.Referer) > 2048 {
		return ytdlpSafeSettings{}, fmt.Errorf("ytdlp_safe.referer is too long")
	}
	if out.SocketTimeout < 0 || out.SocketTimeout > 3600 {
		return ytdlpSafeSettings{}, fmt.Errorf("ytdlp_safe.socket_timeout must be between 0 and 3600")
	}

	if len(in.Headers) > 0 {
		if len(in.Headers) > 30 {
			return ytdlpSafeSettings{}, fmt.Errorf("ytdlp_safe.headers exceeds limit")
		}
		out.Headers = make(map[string]string, len(in.Headers))
		for rawKey, rawVal := range in.Headers {
			key := strings.TrimSpace(rawKey)
			val := strings.TrimSpace(rawVal)
			if key == "" {
				return ytdlpSafeSettings{}, fmt.Errorf("ytdlp_safe.headers contains empty key")
			}
			if len(key) > 128 {
				return ytdlpSafeSettings{}, fmt.Errorf("ytdlp_safe.headers key is too long")
			}
			if len(val) > 2048 {
				return ytdlpSafeSettings{}, fmt.Errorf("ytdlp_safe.headers value is too long")
			}
			out.Headers[key] = val
		}
	}
	return out, nil
}

func parseYTDLPSafeJSON(raw string) (ytdlpSafeSettings, error) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return ytdlpSafeSettings{}, nil
	}
	var cfg ytdlpSafeSettings
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return ytdlpSafeSettings{}, fmt.Errorf("invalid ytdlp_safe_json")
	}
	return normalizeYTDLPSafeSettings(cfg)
}

func buildYTDLPArgsFromSafe(cfg ytdlpSafeSettings) (metadataArgs []string, downloadArgs []string, err error) {
	normalized, err := normalizeYTDLPSafeSettings(cfg)
	if err != nil {
		return nil, nil, err
	}

	common := make([]string, 0, 16)
	if normalized.ExtractorArgs != "" {
		common = append(common, "--extractor-args", normalized.ExtractorArgs)
	}
	if normalized.UserAgent != "" {
		common = append(common, "--user-agent", normalized.UserAgent)
	}
	if normalized.Referer != "" {
		common = append(common, "--referer", normalized.Referer)
	}
	if normalized.SocketTimeout > 0 {
		common = append(common, "--socket-timeout", strconv.FormatInt(normalized.SocketTimeout, 10))
	}
	if len(normalized.Headers) > 0 {
		keys := make([]string, 0, len(normalized.Headers))
		for key := range normalized.Headers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			common = append(common, "--add-header", fmt.Sprintf("%s: %s", key, normalized.Headers[key]))
		}
	}

	metadataArgs = append(metadataArgs, common...)
	downloadArgs = append(downloadArgs, common...)
	if normalized.Format != "" {
		downloadArgs = append(downloadArgs, "--format", normalized.Format)
	}
	if err := validateYTDLPArgTokens(metadataArgs); err != nil {
		return nil, nil, err
	}
	if err := validateYTDLPArgTokens(downloadArgs); err != nil {
		return nil, nil, err
	}
	return metadataArgs, downloadArgs, nil
}

func marshalArgTokenJSON(args []string) (string, error) {
	if args == nil {
		args = []string{}
	}
	out, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
