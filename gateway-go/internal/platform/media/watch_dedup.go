package media

import (
	"bytes"
	"image"
	"image/jpeg"
)

const (
	// nearDupThumbSize is the grayscale thumbnail side length used for
	// near-duplicate detection. Matching Claude Video's 16×16 pass keeps the
	// comparison cheap while catching held slides / static screen recordings.
	nearDupThumbSize = 16
	// nearDupMADThreshold is the mean absolute brightness difference (0–255)
	// below which a frame is treated as a near-duplicate of the last kept one.
	nearDupMADThreshold = 2.0
)

// dedupNearDuplicateFrames drops frames that are visually near-identical to the
// last kept frame (16×16 grayscale mean-absolute-difference). Timestamps stay
// aligned with the surviving frames. On decode failure a frame is kept so a
// bad JPEG never silently empties the set.
func dedupNearDuplicateFrames(frames [][]byte, timestamps []float64) ([][]byte, []float64) {
	if len(frames) <= 1 {
		return frames, timestamps
	}
	n := len(frames)
	if len(timestamps) < n {
		n = len(timestamps)
	}

	keptFrames := make([][]byte, 0, n)
	keptTS := make([]float64, 0, n)
	var lastThumb []byte

	for i := 0; i < n; i++ {
		thumb, err := grayThumb16(frames[i])
		if err != nil {
			keptFrames = append(keptFrames, frames[i])
			keptTS = append(keptTS, timestamps[i])
			lastThumb = nil // next comparison has no reliable reference
			continue
		}
		if lastThumb != nil && meanAbsDiff(lastThumb, thumb) <= nearDupMADThreshold {
			continue
		}
		keptFrames = append(keptFrames, frames[i])
		keptTS = append(keptTS, timestamps[i])
		lastThumb = thumb
	}
	return keptFrames, keptTS
}

// downsampleFramePairs thins (frames, timestamps) to at most n entries while
// always keeping the first and last — same policy as evenSampleTimestamps.
func downsampleFramePairs(frames [][]byte, timestamps []float64, n int) ([][]byte, []float64) {
	if n <= 0 || len(frames) <= n {
		return frames, timestamps
	}
	idxs := evenSampleTimestamps(indexFloats(len(frames)), n)
	outF := make([][]byte, 0, len(idxs))
	outT := make([]float64, 0, len(idxs))
	for _, v := range idxs {
		i := int(v)
		outF = append(outF, frames[i])
		outT = append(outT, timestamps[i])
	}
	return outF, outT
}

func indexFloats(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(i)
	}
	return out
}

func grayThumb16(jpegBytes []byte) ([]byte, error) {
	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, image.ErrFormat
	}
	out := make([]byte, nearDupThumbSize*nearDupThumbSize)
	for y := 0; y < nearDupThumbSize; y++ {
		sy := bounds.Min.Y + y*h/nearDupThumbSize
		if sy >= bounds.Max.Y {
			sy = bounds.Max.Y - 1
		}
		for x := 0; x < nearDupThumbSize; x++ {
			sx := bounds.Min.X + x*w/nearDupThumbSize
			if sx >= bounds.Max.X {
				sx = bounds.Max.X - 1
			}
			r, g, b, _ := img.At(sx, sy).RGBA()
			// Rec. 601 luma; RGBA() is 16-bit, shift to 8-bit.
			out[y*nearDupThumbSize+x] = byte(((r*299 + g*587 + b*114) / 1000) >> 8)
		}
	}
	return out, nil
}

func meanAbsDiff(a, b []byte) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 255
	}
	var sum int
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return float64(sum) / float64(len(a))
}
