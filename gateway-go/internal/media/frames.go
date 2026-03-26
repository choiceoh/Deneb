package media

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// Frame extraction constants.
const (
	// maxFrames is the maximum number of frames to extract from a video.
	// Keeps LLM input tokens reasonable while providing good coverage.
	maxFrames = 6

	// minFrames is the minimum number of frames for short videos.
	minFrames = 1

	// jpegQuality is the ffmpeg JPEG output quality (2 = high quality, 31 = low).
	jpegQuality = 5
)

// ExtractFrames extracts representative JPEG frames from video data using ffmpeg.
// duration is the video length in seconds (from Telegram metadata).
// Returns up to maxFrames JPEG-encoded images.
//
// Frame selection strategy:
//   - For videos <= 3s: extract 1 frame from the middle
//   - For videos <= 10s: extract 3 frames (evenly spaced)
//   - For videos <= 60s: extract 4 frames
//   - For videos > 60s: extract 6 frames (evenly spaced)
func ExtractFrames(videoData []byte, duration int) ([][]byte, error) {
	if len(videoData) == 0 {
		return nil, fmt.Errorf("empty video data")
	}

	// Write video to temp file.
	tmpDir, err := os.MkdirTemp("", "deneb-frames-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(videoPath, videoData, 0o600); err != nil {
		return nil, fmt.Errorf("write temp video: %w", err)
	}

	// Determine number of frames based on duration.
	numFrames := selectFrameCount(duration)

	// Calculate timestamps for frame extraction.
	timestamps := selectTimestamps(duration, numFrames)

	// Extract frames at each timestamp.
	var frames [][]byte
	for i, ts := range timestamps {
		outPath := filepath.Join(tmpDir, fmt.Sprintf("frame_%03d.jpg", i))
		args := []string{
			"-ss", fmt.Sprintf("%.2f", ts),
			"-i", videoPath,
			"-vframes", "1",
			"-q:v", fmt.Sprintf("%d", jpegQuality),
			"-y",
			outPath,
		}

		cmd := exec.Command("ffmpeg", args...)
		cmd.Stderr = nil // suppress ffmpeg stderr noise
		cmd.Stdout = nil
		if err := cmd.Run(); err != nil {
			// Skip this frame on error; try the rest.
			continue
		}

		data, err := os.ReadFile(outPath)
		if err != nil || len(data) == 0 {
			continue
		}
		frames = append(frames, data)
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames extracted (ffmpeg may not be available)")
	}

	return frames, nil
}

// selectFrameCount determines how many frames to extract based on video duration.
func selectFrameCount(duration int) int {
	switch {
	case duration <= 3:
		return 1
	case duration <= 10:
		return 3
	case duration <= 60:
		return 4
	default:
		return maxFrames
	}
}

// selectTimestamps generates evenly-spaced timestamps across the video duration.
// Avoids the very start (0s) and very end to get more representative content.
func selectTimestamps(duration, count int) []float64 {
	if duration <= 0 {
		// Unknown duration: just grab the first second.
		return []float64{0.5}
	}

	d := float64(duration)
	if count == 1 {
		return []float64{d / 2}
	}

	// Offset from edges: 5% of duration, minimum 0.5s, maximum 2s.
	offset := d * 0.05
	if offset < 0.5 {
		offset = 0.5
	}
	if offset > 2.0 {
		offset = 2.0
	}

	usable := d - 2*offset
	if usable < 0 {
		usable = 0
		offset = d / 2
	}

	timestamps := make([]float64, count)
	for i := 0; i < count; i++ {
		if count == 1 {
			timestamps[i] = offset
		} else {
			timestamps[i] = offset + usable*float64(i)/float64(count-1)
		}
	}

	sort.Float64s(timestamps)
	return timestamps
}
