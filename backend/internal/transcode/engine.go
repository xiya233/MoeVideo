package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type VariantInfo struct {
	Name              string `json:"name"`
	Width             int64  `json:"width"`
	Height            int64  `json:"height"`
	Bandwidth         int64  `json:"bandwidth"`
	PlaylistObjectKey string `json:"playlist_object_key"`
}

type BuildResult struct {
	MasterPlaylist string
	Variants       []VariantInfo
	SegmentSeconds int64
}

type Engine interface {
	BuildHLS(ctx context.Context, inputPath, outputDir string, segmentSeconds int64) (*BuildResult, error)
	GenerateCover(ctx context.Context, inputPath, outputPath string) error
	GeneratePreviewWebP(ctx context.Context, inputPath, outputPath string) error
	GenerateVTTThumbnail(ctx context.Context, inputPath, vttOutputPath, spriteOutputPath string) error
}

type FFmpegEngine struct {
	ffmpegBin  string
	ffprobeBin string
}

func NewFFmpegEngine(ffmpegBin, ffprobeBin string) *FFmpegEngine {
	if strings.TrimSpace(ffmpegBin) == "" {
		ffmpegBin = "ffmpeg"
	}
	if strings.TrimSpace(ffprobeBin) == "" {
		ffprobeBin = "ffprobe"
	}
	return &FFmpegEngine{
		ffmpegBin:  ffmpegBin,
		ffprobeBin: ffprobeBin,
	}
}

type ladderPreset struct {
	Name         string
	Height       int
	VideoBitrate int
	MaxRate      int
	BufSize      int
	AudioBitrate int
}

var baseLadder = []ladderPreset{
	{Name: "360p", Height: 360, VideoBitrate: 800_000, MaxRate: 856_000, BufSize: 1_200_000, AudioBitrate: 96_000},
	{Name: "480p", Height: 480, VideoBitrate: 1_400_000, MaxRate: 1_498_000, BufSize: 2_100_000, AudioBitrate: 128_000},
	{Name: "720p", Height: 720, VideoBitrate: 2_800_000, MaxRate: 2_996_000, BufSize: 4_200_000, AudioBitrate: 128_000},
	{Name: "1080p", Height: 1080, VideoBitrate: 5_000_000, MaxRate: 5_350_000, BufSize: 7_500_000, AudioBitrate: 192_000},
}

func (e *FFmpegEngine) BuildHLS(ctx context.Context, inputPath, outputDir string, segmentSeconds int64) (*BuildResult, error) {
	width, height, err := e.probeVideoResolution(ctx, inputPath)
	if err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid source resolution: %dx%d", width, height)
	}

	presets := selectLadder(height)
	if len(presets) == 0 {
		presets = []ladderPreset{fallbackPreset(height)}
	}

	result := &BuildResult{
		MasterPlaylist: "master.m3u8",
		Variants:       make([]VariantInfo, 0, len(presets)),
		SegmentSeconds: segmentSeconds,
	}
	videoEncoder := e.pickH264Encoder(ctx)

	for _, preset := range presets {
		targetHeight := makeEven(preset.Height)
		targetWidth := makeEven(int(math.Round(float64(width) * float64(targetHeight) / float64(height))))
		if targetWidth <= 0 || targetHeight <= 0 {
			return nil, fmt.Errorf("invalid target resolution for %s: %dx%d", preset.Name, targetWidth, targetHeight)
		}

		variantDir := filepath.Join(outputDir, preset.Name)
		if err := os.MkdirAll(variantDir, 0o755); err != nil {
			return nil, fmt.Errorf("create variant dir %s: %w", variantDir, err)
		}
		playlistPath := filepath.Join(variantDir, "index.m3u8")
		segmentPattern := filepath.Join(variantDir, "seg_%03d.ts")

		ffArgs := []string{
			"-y",
			"-i", inputPath,
			"-map", "0:v:0",
			"-map", "0:a:0?",
			"-vf", fmt.Sprintf("scale=%d:%d", targetWidth, targetHeight),
			"-c:v", videoEncoder,
			"-pix_fmt", "yuv420p",
			"-g", "48",
			"-keyint_min", "48",
			"-b:v", toKbps(preset.VideoBitrate),
			"-maxrate", toKbps(preset.MaxRate),
			"-bufsize", toKbps(preset.BufSize),
			"-c:a", "aac",
			"-ac", "2",
			"-ar", "48000",
			"-b:a", toKbps(preset.AudioBitrate),
			"-hls_time", strconv.FormatInt(segmentSeconds, 10),
			"-hls_playlist_type", "vod",
			"-hls_flags", "independent_segments",
			"-hls_segment_filename", segmentPattern,
			playlistPath,
		}
		if videoEncoder == "libx264" {
			// Keep x264-specific tuning only when libx264 is available.
			ffArgs = insertAfterValue(ffArgs, "-c:v", []string{
				"-preset", "veryfast",
				"-profile:v", "main",
				"-crf", "20",
				"-sc_threshold", "0",
			})
		}
		if err := runCommand(ctx, e.ffmpegBin, ffArgs...); err != nil {
			return nil, fmt.Errorf("transcode %s failed: %w", preset.Name, err)
		}

		result.Variants = append(result.Variants, VariantInfo{
			Name:              preset.Name,
			Width:             int64(targetWidth),
			Height:            int64(targetHeight),
			Bandwidth:         int64(preset.VideoBitrate + preset.AudioBitrate),
			PlaylistObjectKey: filepath.ToSlash(filepath.Join(preset.Name, "index.m3u8")),
		})
	}

	sort.Slice(result.Variants, func(i, j int) bool {
		return result.Variants[i].Height < result.Variants[j].Height
	})

	if err := writeMasterPlaylist(filepath.Join(outputDir, result.MasterPlaylist), result.Variants); err != nil {
		return nil, err
	}

	return result, nil
}

func (e *FFmpegEngine) probeVideoResolution(ctx context.Context, inputPath string) (int, int, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "json",
		inputPath,
	}
	output, err := exec.CommandContext(ctx, e.ffprobeBin, args...).CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ffprobe failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	var parsed struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return 0, 0, fmt.Errorf("parse ffprobe output: %w", err)
	}
	if len(parsed.Streams) == 0 {
		return 0, 0, fmt.Errorf("video stream not found")
	}
	return parsed.Streams[0].Width, parsed.Streams[0].Height, nil
}

func (e *FFmpegEngine) GenerateCover(ctx context.Context, inputPath, outputPath string) error {
	args := []string{
		"-y",
		"-ss", "3",
		"-i", inputPath,
		"-frames:v", "1",
		"-q:v", "2",
		outputPath,
	}
	if err := runCommand(ctx, e.ffmpegBin, args...); err != nil {
		return fmt.Errorf("generate cover: %w", err)
	}
	return nil
}

func (e *FFmpegEngine) GeneratePreviewWebP(ctx context.Context, inputPath, outputPath string) error {
	args := []string{
		"-y",
		"-ss", "1",
		"-t", "3",
		"-i", inputPath,
		"-vf", "fps=12,scale=320:-1:flags=lanczos",
		"-loop", "0",
		"-an",
		outputPath,
	}
	if err := runCommand(ctx, e.ffmpegBin, args...); err != nil {
		return fmt.Errorf("generate preview webp: %w", err)
	}
	return nil
}

func (e *FFmpegEngine) GenerateVTTThumbnail(ctx context.Context, inputPath, vttOutputPath, spriteOutputPath string) error {
	duration, err := e.probeDurationSec(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("probe duration for vtt thumbnail: %w", err)
	}
	if duration <= 0 {
		return fmt.Errorf("invalid video duration for vtt thumbnail: %.3f", duration)
	}

	const (
		intervalSec = 10.0
		thumbWidth  = 160
		thumbHeight = 90
		tileCols    = 10
	)

	frameCount := int(math.Ceil(duration / intervalSec))
	if frameCount < 1 {
		frameCount = 1
	}
	tileRows := int(math.Ceil(float64(frameCount) / float64(tileCols)))
	if tileRows < 1 {
		tileRows = 1
	}

	filter := fmt.Sprintf(
		"fps=1/%.0f,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black,tile=%dx%d",
		intervalSec,
		thumbWidth,
		thumbHeight,
		thumbWidth,
		thumbHeight,
		tileCols,
		tileRows,
	)
	args := []string{
		"-y",
		"-i", inputPath,
		"-vf", filter,
		"-frames:v", "1",
		"-q:v", "4",
		spriteOutputPath,
	}
	if err := runCommand(ctx, e.ffmpegBin, args...); err != nil {
		return fmt.Errorf("generate sprite jpg: %w", err)
	}

	spriteName := filepath.Base(spriteOutputPath)
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for idx := 0; idx < frameCount; idx++ {
		start := float64(idx) * intervalSec
		if start >= duration {
			break
		}
		end := float64(idx+1) * intervalSec
		if end > duration {
			end = duration
		}
		x := (idx % tileCols) * thumbWidth
		y := (idx / tileCols) * thumbHeight
		b.WriteString(formatVTTTimestamp(start))
		b.WriteString(" --> ")
		b.WriteString(formatVTTTimestamp(end))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%s#xywh=%d,%d,%d,%d\n\n", spriteName, x, y, thumbWidth, thumbHeight))
	}

	if err := os.WriteFile(vttOutputPath, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write vtt thumbnail: %w", err)
	}
	return nil
}

func runCommand(ctx context.Context, bin string, args ...string) error {
	output, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		const maxOutput = 4000
		if len(msg) > maxOutput {
			keepHead := 800
			keepTail := maxOutput - keepHead - 40
			msg = msg[:keepHead] + "\n...[truncated]...\n" + msg[len(msg)-keepTail:]
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func (e *FFmpegEngine) pickH264Encoder(ctx context.Context) string {
	encoders, err := exec.CommandContext(ctx, e.ffmpegBin, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil {
		return "libx264"
	}
	output := string(encoders)
	if strings.Contains(output, " libx264 ") {
		return "libx264"
	}
	if strings.Contains(output, " libopenh264 ") {
		return "libopenh264"
	}
	return "libx264"
}

func (e *FFmpegEngine) probeDurationSec(ctx context.Context, inputPath string) (float64, error) {
	args := []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	}
	output, err := exec.CommandContext(ctx, e.ffprobeBin, args...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return 0, fmt.Errorf("empty duration output")
	}
	duration, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", raw, err)
	}
	return duration, nil
}

func formatVTTTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	totalMillis := int64(math.Round(seconds * 1000))
	hours := totalMillis / 3_600_000
	minutes := (totalMillis % 3_600_000) / 60_000
	secs := (totalMillis % 60_000) / 1000
	millis := totalMillis % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, millis)
}

func insertAfterValue(args []string, marker string, insert []string) []string {
	out := make([]string, 0, len(args)+len(insert))
	for i := 0; i < len(args); i++ {
		out = append(out, args[i])
		if args[i] == marker && i+1 < len(args) {
			out = append(out, args[i+1])
			out = append(out, insert...)
			i++
		}
	}
	return out
}

func writeMasterPlaylist(path string, variants []VariantInfo) error {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	for _, v := range variants {
		b.WriteString(fmt.Sprintf(
			"#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS=\"avc1.64001f,mp4a.40.2\"\n",
			v.Bandwidth,
			v.Width,
			v.Height,
		))
		b.WriteString(v.PlaylistObjectKey + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write master playlist: %w", err)
	}
	return nil
}

func selectLadder(sourceHeight int) []ladderPreset {
	out := make([]ladderPreset, 0, len(baseLadder))
	for _, p := range baseLadder {
		if p.Height <= sourceHeight {
			out = append(out, p)
		}
	}
	return out
}

func fallbackPreset(sourceHeight int) ladderPreset {
	h := makeEven(sourceHeight)
	if h <= 0 {
		h = 240
	}
	return ladderPreset{
		Name:         fmt.Sprintf("%dp", h),
		Height:       h,
		VideoBitrate: 600_000,
		MaxRate:      650_000,
		BufSize:      900_000,
		AudioBitrate: 96_000,
	}
}

func makeEven(v int) int {
	if v <= 0 {
		return 2
	}
	if v%2 == 0 {
		return v
	}
	return v - 1
}

func toKbps(v int) string {
	if v <= 0 {
		return "256k"
	}
	return fmt.Sprintf("%dk", v/1000)
}
