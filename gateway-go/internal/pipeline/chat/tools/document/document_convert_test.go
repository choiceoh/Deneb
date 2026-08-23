package document

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The conversion success paths shell out to hwp5txt / LibreOffice, which live on
// the deploy host (like the OCR/ASR sidecars), not in CI — so these lock the
// tool-independent dispatch: unknown formats stay unsupported, and each convertible
// format targets the OOXML that best preserves it.

func TestConvertUnsupportedUnknownExtensionStaysUnsupported(t *testing.T) {
	r := convertUnsupported(context.Background(), []byte("whatever"), "archive.zip")
	if r.kind != docUnsupported {
		t.Fatalf("unknown extension = %v, want docUnsupported", r.kind)
	}
}

func TestExtractDocumentUnknownBinaryStaysUnsupported(t *testing.T) {
	// A format with no native parser and no converter must degrade to unsupported,
	// not error or misclassify — the pre-conversion behavior is preserved.
	r := extractDocument(context.Background(), []byte{0x00, 0x01, 0x02}, "mystery.bin", "application/octet-stream")
	if r.kind != docUnsupported {
		t.Fatalf("unknown binary = %v, want docUnsupported", r.kind)
	}
}

func TestSofficeTargetMapping(t *testing.T) {
	for _, tc := range []struct{ ext, want string }{
		{".doc", "docx"},
		{".rtf", "docx"},
		{".odt", "docx"},
		{".hwp", "docx"},
		{".hwpx", "docx"},
		{".xls", "xlsx"},
		{".ods", "xlsx"},
		{".ppt", "pptx"},
		{".odp", "pptx"},
	} {
		if got := sofficeTarget(tc.ext); got != tc.want {
			t.Errorf("sofficeTarget(%q) = %q, want %q", tc.ext, got, tc.want)
		}
	}
}

func TestRunBoundedTextCommandCapsConverterOutput(t *testing.T) {
	t.Run("stdout", func(t *testing.T) {
		_, err := runBoundedTextCommand(context.Background(), convertTimeout, nil, func(_ context.Context, _ io.Reader, stdout, _ io.Writer) error {
			_, copyErr := io.CopyN(stdout, repeatingByteReader('x'), maxDocumentOutputBytes+1)
			return copyErr
		})
		if !errors.Is(err, errDocumentExtractionLimit) {
			t.Fatalf("error = %v, want converter stdout limit", err)
		}
	})

	t.Run("stderr", func(t *testing.T) {
		_, err := runBoundedTextCommand(context.Background(), convertTimeout, nil, func(_ context.Context, _ io.Reader, _, stderr io.Writer) error {
			if _, writeErr := io.CopyN(stderr, repeatingByteReader('e'), maxSubprocessStderrBytes+1024); writeErr != nil {
				return writeErr
			}
			return errors.New("fake converter failure")
		})
		if err == nil || len(err.Error()) != maxSubprocessStderrBytes || strings.Trim(err.Error(), "e") != "" {
			t.Fatalf("bounded stderr error bytes=%d error=%v", len(err.Error()), err)
		}
	})
}

func TestReadBoundedRegularFileRejectsOversizeConvertedOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "converted.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxDocumentOutputBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedRegularFile(path, maxDocumentOutputBytes); !errors.Is(err, errDocumentExtractionLimit) {
		t.Fatalf("error = %v, want converted-file size limit", err)
	}
}

func TestConvertUnsupportedRejectsCompressedArchiveBombBeforeSoffice(t *testing.T) {
	data := makeCompressedOOXMLPart(t, "content.xml", "", "", maxConverterArchivePartBytes+1)
	for _, filename := range []string{
		"bomb.odt", "renamed.doc", "renamed.rtf", "renamed.xls",
		"renamed.ppt", "renamed.hwp", "renamed.wps",
	} {
		t.Run(filename, func(t *testing.T) {
			r := convertUnsupported(context.Background(), data, filename)
			if r.kind != docUnsupported || !errors.Is(r.err, errDocumentExtractionLimit) {
				t.Fatalf("kind=%v error=%v, want pre-converter archive limit", r.kind, r.err)
			}
		})
	}
	if err := preflightConverterInput([]byte("legacy binary payload"), ".doc"); err != nil {
		t.Fatalf("non-ZIP legacy payload rejected: %v", err)
	}
}

func TestValidateConverterArchiveMetadataBoundsCountAndTotal(t *testing.T) {
	t.Run("entries", func(t *testing.T) {
		zr := &zip.Reader{}
		for i := 0; i <= maxConverterArchiveParts; i++ {
			zr.File = append(zr.File, &zip.File{FileHeader: zip.FileHeader{Name: fmt.Sprintf("part-%d", i)}})
		}
		if err := validateConverterArchiveMetadata(zr); !errors.Is(err, errDocumentExtractionLimit) {
			t.Fatalf("error = %v, want converter entry limit", err)
		}
	})

	t.Run("aggregate", func(t *testing.T) {
		zr := &zip.Reader{}
		for i := 0; i < maxConverterArchiveTotal/maxConverterArchivePartBytes+1; i++ {
			zr.File = append(zr.File, &zip.File{FileHeader: zip.FileHeader{
				Name:               fmt.Sprintf("part-%d", i),
				UncompressedSize64: maxConverterArchivePartBytes,
			}})
		}
		if err := validateConverterArchiveMetadata(zr); !errors.Is(err, errDocumentExtractionLimit) {
			t.Fatalf("error = %v, want converter aggregate limit", err)
		}
	})
}
