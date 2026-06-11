package media

import (
	"sort"
)

// Frame extraction constants.
const (
	// maxFrames is the maximum number of frames to extract from a video.
	// Keeps LLM input tokens reasonable while providing good coverage.
	maxFrames = 6

	// Duration thresholds (seconds) for frame count selection.
	durationShort  = 3  // <= 3s: 1 frame
	durationMedium = 10 // <= 10s: 3 frames
	durationLong   = 60 // <= 60s: 4 frames

	// Edge offset for timestamp placement (avoids grabbing very start/end).
	edgeOffsetRatio = 0.05 // 5% of duration
	edgeOffsetMin   = 0.5  // minimum 0.5s
	edgeOffsetMax   = 2.0  // maximum 2.0s
)

// selectFrameCount determines how many frames to extract based on video duration.
func selectFrameCount(duration int) int {
	switch {
	case duration <= durationShort:
		return 1
	case duration <= durationMedium:
		return 3
	case duration <= durationLong:
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

	// Offset from edges to avoid grabbing black frames at start/end.
	offset := d * edgeOffsetRatio
	if offset < edgeOffsetMin {
		offset = edgeOffsetMin
	}
	if offset > edgeOffsetMax {
		offset = edgeOffsetMax
	}

	usable := d - 2*offset
	if usable < 0 {
		usable = 0
		offset = d / 2
	}

	timestamps := make([]float64, count)
	for i := range count {
		if count == 1 {
			timestamps[i] = offset
		} else {
			timestamps[i] = offset + usable*float64(i)/float64(count-1)
		}
	}

	sort.Float64s(timestamps)
	return timestamps
}
