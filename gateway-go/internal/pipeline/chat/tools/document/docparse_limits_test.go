package document

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func makeCompressedOOXMLPart(t *testing.T, name, prefix, suffix string, repeatedBytes int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	w, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, prefix); err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte{'A'}, 32<<10)
	for repeatedBytes > 0 {
		writeBytes := int64(len(chunk))
		if repeatedBytes < writeBytes {
			writeBytes = repeatedBytes
		}
		if _, err := w.Write(chunk[:writeBytes]); err != nil {
			t.Fatal(err)
		}
		repeatedBytes -= writeBytes
	}
	if _, err := io.WriteString(w, suffix); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractFileTextWithoutOCRRejectsCompressedOversizeOOXMLParts(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		part     string
	}{
		{name: "docx", filename: "oversize.docx", part: "word/document.xml"},
		{name: "xlsx", filename: "oversize.xlsx", part: "xl/worksheets/sheet1.xml"},
		{name: "xlsm", filename: "oversize.xlsm", part: "xl/worksheets/sheet1.xml"},
		{name: "pptx", filename: "oversize.pptx", part: "ppt/slides/slide1.xml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := makeCompressedOOXMLPart(t, tt.part, "", "", maxOOXMLPartBytes+1)
			if len(data) >= 1<<20 {
				t.Fatalf("fixture is not highly compressed: %d bytes", len(data))
			}
			if text, complete := ExtractFileTextWithoutOCR(context.Background(), tt.filename, data); text != "" || complete {
				t.Fatalf("text bytes=%d complete=%v, want bounded incomplete extraction", len(text), complete)
			}
		})
	}
}

func TestExtractFileTextWithoutOCRRejectsOOXMLOutputAmplification(t *testing.T) {
	data := makeCompressedOOXMLPart(
		t,
		"word/document.xml",
		"<document><p><t>",
		"</t></p></document>",
		maxOOXMLOutputBytes+1,
	)
	if text, complete := ExtractFileTextWithoutOCR(context.Background(), "amplified.docx", data); text != "" || complete {
		t.Fatalf("text bytes=%d complete=%v, want output-limited incomplete extraction", len(text), complete)
	}
}

func TestExtractFileTextWithoutOCRRejectsMalformedOOXML(t *testing.T) {
	tests := []struct {
		filename string
		part     string
		content  string
	}{
		{filename: "broken.docx", part: "word/document.xml", content: `<document><p><t>prefix</t>`},
		{filename: "broken.xlsx", part: "xl/worksheets/sheet1.xml", content: `<worksheet>`},
		{filename: "broken.pptx", part: "ppt/slides/slide1.xml", content: `<sld><p><t>prefix</t>`},
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			data := makeContractZip(t, map[string]string{tt.part: tt.content})
			if text, complete := ExtractFileTextWithoutOCR(context.Background(), tt.filename, data); text != "" || complete {
				t.Fatalf("text=%q complete=%v, want malformed input marked incomplete", text, complete)
			}
		})
	}
}

func TestCollectOOXMLPartsRejectsCountAndAggregateLimits(t *testing.T) {
	t.Run("relevant part count", func(t *testing.T) {
		zr := &zip.Reader{}
		for i := 0; i <= maxOOXMLRelevantParts; i++ {
			zr.File = append(zr.File, &zip.File{FileHeader: zip.FileHeader{Name: fmt.Sprintf("part-%d.xml", i)}})
		}
		_, err := collectOOXMLParts(zr, func(string) bool { return true })
		if !errors.Is(err, errDocumentExtractionLimit) {
			t.Fatalf("error = %v, want relevant-part limit", err)
		}
	})

	t.Run("aggregate uncompressed bytes", func(t *testing.T) {
		zr := &zip.Reader{}
		for i := 0; i < maxOOXMLTotalBytes/maxOOXMLPartBytes+1; i++ {
			zr.File = append(zr.File, &zip.File{FileHeader: zip.FileHeader{
				Name:               fmt.Sprintf("part-%d.xml", i),
				UncompressedSize64: maxOOXMLPartBytes,
			}})
		}
		_, err := collectOOXMLParts(zr, func(string) bool { return true })
		if !errors.Is(err, errDocumentExtractionLimit) {
			t.Fatalf("error = %v, want aggregate byte limit", err)
		}
	})
}

type repeatingByteReader byte

func (r repeatingByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

func TestRunPDFToTextBoundsCapturedOutputWithoutPoppler(t *testing.T) {
	t.Run("stdout hard limit", func(t *testing.T) {
		text, err := runPDFToText(context.Background(), []byte("pdf bytes"), func(_ context.Context, stdin io.Reader, stdout, _ io.Writer) error {
			input, readErr := io.ReadAll(stdin)
			if readErr != nil {
				return readErr
			}
			if string(input) != "pdf bytes" {
				return fmt.Errorf("stdin = %q", input)
			}
			_, copyErr := io.CopyN(stdout, repeatingByteReader('x'), maxPDFTextOutputBytes+1)
			return copyErr
		})
		if text != "" || !errors.Is(err, errDocumentExtractionLimit) {
			t.Fatalf("text bytes=%d error=%v, want stdout limit", len(text), err)
		}
	})

	t.Run("stderr retained prefix", func(t *testing.T) {
		_, err := runPDFToText(context.Background(), nil, func(_ context.Context, _ io.Reader, _, stderr io.Writer) error {
			if _, writeErr := io.CopyN(stderr, repeatingByteReader('e'), maxPDFTextStderrBytes+1024); writeErr != nil {
				return writeErr
			}
			return errors.New("fake pdftotext failure")
		})
		if err == nil || len(err.Error()) != maxPDFTextStderrBytes || strings.Trim(err.Error(), "e") != "" {
			t.Fatalf("bounded stderr error bytes=%d error prefix=%q", len(err.Error()), err)
		}
	})
}

func TestRunPDFToTextPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runPDFToText(ctx, nil, func(runCtx context.Context, _ io.Reader, _, _ io.Writer) error {
		<-runCtx.Done()
		return runCtx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}
