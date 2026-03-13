package media

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

func ProbeVideoFileMetadata(ctx context.Context, ffprobeBin, inputPath string) (int64, int64, int64, error) {
	bin := strings.TrimSpace(ffprobeBin)
	if bin == "" {
		bin = "ffprobe"
	}

	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration",
		"-of", "json",
		inputPath,
	}
	output, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("ffprobe failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	var parsed struct {
		Streams []struct {
			Width  int64 `json:"width"`
			Height int64 `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return 0, 0, 0, fmt.Errorf("parse ffprobe output: %w", err)
	}

	durationSec := int64(0)
	if parsed.Format.Duration != "" {
		d, err := strconv.ParseFloat(parsed.Format.Duration, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse duration: %w", err)
		}
		if d > 0 {
			durationSec = int64(math.Round(d))
		}
	}

	width := int64(0)
	height := int64(0)
	if len(parsed.Streams) > 0 {
		width = parsed.Streams[0].Width
		height = parsed.Streams[0].Height
	}

	return durationSec, width, height, nil
}
