package document

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

func pngHeader(width, height uint32) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], width)
	binary.BigEndian.PutUint32(data[4:8], height)
	data[8] = 8 // bit depth
	data[9] = 2 // truecolor
	binary.Write(&out, binary.BigEndian, uint32(len(data)))
	chunk := append([]byte("IHDR"), data...)
	out.Write(chunk)
	binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return out.Bytes()
}

func TestValidateOCRImageRejectsDimensionAndPixelAmplification(t *testing.T) {
	if err := validateOCRImage(pngHeader(1, 1)); err != nil {
		t.Fatalf("1x1 PNG header rejected: %v", err)
	}
	for _, tc := range []struct {
		name          string
		width, height uint32
	}{
		{name: "dimension", width: maxOCRImageDimension + 1, height: 1},
		{name: "pixels", width: 4000, height: 4000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateOCRImage(pngHeader(tc.width, tc.height)); !errors.Is(err, errDocumentExtractionLimit) {
				t.Fatalf("error = %v, want image resource limit", err)
			}
		})
	}
}

func TestValidateOCRImagePreservesExtendedFormatHeaders(t *testing.T) {
	bmp := make([]byte, 26)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[14:18], 40)
	binary.LittleEndian.PutUint32(bmp[18:22], 1)
	binary.LittleEndian.PutUint32(bmp[22:26], 1)

	webp := make([]byte, 30)
	copy(webp[0:4], "RIFF")
	copy(webp[8:12], "WEBP")
	copy(webp[12:16], "VP8X")

	tiff := make([]byte, 38)
	copy(tiff[:4], "II*\x00")
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 2)
	for i, tag := range []uint16{256, 257} {
		offset := 10 + i*12
		binary.LittleEndian.PutUint16(tiff[offset:offset+2], tag)
		binary.LittleEndian.PutUint16(tiff[offset+2:offset+4], 3)
		binary.LittleEndian.PutUint32(tiff[offset+4:offset+8], 1)
		binary.LittleEndian.PutUint16(tiff[offset+8:offset+10], 1)
	}

	for name, data := range map[string][]byte{"bmp": bmp, "webp": webp, "tiff": tiff} {
		t.Run(name, func(t *testing.T) {
			if err := validateOCRImage(data); err != nil {
				t.Fatalf("extended image header rejected: %v", err)
			}
		})
	}
}

func TestImageOCRRejectsMultipageTIFFBeforeProvider(t *testing.T) {
	// First IFD is a harmless 1x1 page; the second advertises an oversized page.
	// The OCR facade rejects multipage TIFFs before tesseract can walk the chain.
	tiff := make([]byte, 68)
	copy(tiff[:4], "II*\x00")
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	writeIFD := func(offset int, width, height uint32, next uint32) {
		binary.LittleEndian.PutUint16(tiff[offset:offset+2], 2)
		for i, value := range []uint32{width, height} {
			entry := offset + 2 + i*12
			binary.LittleEndian.PutUint16(tiff[entry:entry+2], uint16(256+i))
			binary.LittleEndian.PutUint16(tiff[entry+2:entry+4], 4)
			binary.LittleEndian.PutUint32(tiff[entry+4:entry+8], 1)
			binary.LittleEndian.PutUint32(tiff[entry+8:entry+12], value)
		}
		binary.LittleEndian.PutUint32(tiff[offset+26:offset+30], next)
	}
	writeIFD(8, 1, 1, 38)
	writeIFD(38, maxOCRImageDimension+1, 1, 0)

	text, err := imageOCR(context.Background(), tiff)
	if text != "" || !errors.Is(err, errDocumentExtractionLimit) {
		t.Fatalf("text=%q error=%v, want pre-provider multipage TIFF limit", text, err)
	}
}

func TestImageOCRRejectsOversizeHeaderBeforeProvider(t *testing.T) {
	text, err := imageOCR(context.Background(), pngHeader(maxOCRImageDimension+1, 1))
	if text != "" || !errors.Is(err, errDocumentExtractionLimit) {
		t.Fatalf("text=%q error=%v, want pre-provider dimension rejection", text, err)
	}
}

func TestPDFRasterBudgetRejectsOversizeAndRemovesTempFiles(t *testing.T) {
	t.Run("per page bytes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "page-1.png")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(maxPDFRasterPageBytes + 1); err != nil {
			file.Close()
			t.Fatal(err)
		}
		file.Close()
		_, err = (&pdfRasterBudget{}).read(path)
		if !errors.Is(err, errDocumentExtractionLimit) {
			t.Fatalf("error = %v, want raster page byte limit", err)
		}
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("oversize raster was not removed: %v", statErr)
		}
	})

	t.Run("aggregate bytes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "page-2.png")
		if err := os.WriteFile(path, pngHeader(1, 1), 0o600); err != nil {
			t.Fatal(err)
		}
		budget := &pdfRasterBudget{bytes: maxPDFRasterTotalBytes - 1}
		if _, err := budget.read(path); !errors.Is(err, errDocumentExtractionLimit) {
			t.Fatalf("error = %v, want raster aggregate limit", err)
		}
	})

	t.Run("decoded dimensions", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "page-3.png")
		if err := os.WriteFile(path, pngHeader(maxOCRImageDimension+1, 1), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := (&pdfRasterBudget{}).read(path); !errors.Is(err, errDocumentExtractionLimit) {
			t.Fatalf("error = %v, want raster dimension limit", err)
		}
	})
}
