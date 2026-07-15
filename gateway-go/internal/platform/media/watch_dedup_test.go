package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"
)

func solidJPEG(r, g, b uint8, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	col := color.RGBA{R: r, G: g, B: b, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, col)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestDedupNearDuplicateFramesDropsHeldSlides(t *testing.T) {
	red := solidJPEG(200, 20, 20, 64, 48)
	redAgain := solidJPEG(201, 21, 21, 64, 48) // near-identical luma
	blue := solidJPEG(20, 20, 200, 64, 48)

	frames := [][]byte{red, redAgain, blue}
	ts := []float64{0, 1, 2}
	gotF, gotT := dedupNearDuplicateFrames(frames, ts)
	if len(gotF) != 2 || len(gotT) != 2 {
		t.Fatalf("got %d frames / %d timestamps, want 2/2", len(gotF), len(gotT))
	}
	if gotT[0] != 0 || gotT[1] != 2 {
		t.Errorf("timestamps = %v, want [0 2]", gotT)
	}
}

func TestDedupNearDuplicateFramesKeepsDistinctFrames(t *testing.T) {
	a := solidJPEG(0, 0, 0, 32, 32)
	b := solidJPEG(255, 255, 255, 32, 32)
	c := solidJPEG(0, 255, 0, 32, 32)
	gotF, gotT := dedupNearDuplicateFrames(
		[][]byte{a, b, c},
		[]float64{0, 5, 10},
	)
	if len(gotF) != 3 || len(gotT) != 3 {
		t.Fatalf("got %d/%d, want 3/3", len(gotF), len(gotT))
	}
}

func TestDedupNearDuplicateFramesEmptyAndSingle(t *testing.T) {
	if f, ts := dedupNearDuplicateFrames(nil, nil); len(f) != 0 || len(ts) != 0 {
		t.Errorf("empty: got %d/%d", len(f), len(ts))
	}
	one := [][]byte{solidJPEG(10, 10, 10, 16, 16)}
	f, ts := dedupNearDuplicateFrames(one, []float64{1.5})
	if len(f) != 1 || len(ts) != 1 || ts[0] != 1.5 {
		t.Errorf("single: got %d frames ts=%v", len(f), ts)
	}
}

func TestDownsampleFramePairsPreservesEndpoints(t *testing.T) {
	frames := make([][]byte, 10)
	ts := make([]float64, 10)
	for i := range frames {
		frames[i] = []byte{byte(i)}
		ts[i] = float64(i)
	}
	gotF, gotT := downsampleFramePairs(frames, ts, 4)
	if len(gotF) != 4 || len(gotT) != 4 {
		t.Fatalf("got %d/%d, want 4/4", len(gotF), len(gotT))
	}
	if gotT[0] != 0 || gotT[len(gotT)-1] != 9 {
		t.Errorf("endpoints = %v, want first 0 last 9", gotT)
	}
}

func TestMeanAbsDiffIdentical(t *testing.T) {
	a := []byte{10, 20, 30, 40}
	if d := meanAbsDiff(a, a); d != 0 {
		t.Errorf("identical MAD = %f, want 0", d)
	}
	b := []byte{12, 18, 30, 40}
	if d := meanAbsDiff(a, b); math.Abs(d-1) > 1e-9 {
		t.Errorf("MAD = %f, want 1", d)
	}
}

func TestGrayThumb16Decodes(t *testing.T) {
	thumb, err := grayThumb16(solidJPEG(128, 64, 32, 80, 60))
	if err != nil {
		t.Fatal(err)
	}
	if len(thumb) != nearDupThumbSize*nearDupThumbSize {
		t.Fatalf("thumb len = %d, want %d", len(thumb), nearDupThumbSize*nearDupThumbSize)
	}
}
