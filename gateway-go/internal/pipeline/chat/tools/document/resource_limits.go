package document

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // register GIF headers for image.DecodeConfig
	_ "image/jpeg" // register JPEG headers for image.DecodeConfig
	_ "image/png"  // register PNG headers for image.DecodeConfig
	"io"
	"os"
	"strings"
	"time"
)

const (
	maxOCRImageDimension = 8192
	maxOCRImagePixels    = 12_000_000

	maxConverterArchiveParts     = 1024
	maxConverterArchivePartBytes = maxOOXMLPartBytes
	maxConverterArchiveTotal     = maxOOXMLTotalBytes
)

func runBoundedTextCommand(
	ctx context.Context,
	timeout time.Duration,
	input io.Reader,
	run boundedCommandRunner,
) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out := newLimitedBuffer(maxDocumentOutputBytes, false)
	errBuf := newLimitedBuffer(maxSubprocessStderrBytes, true)
	if err := run(runCtx, input, out, errBuf); err != nil {
		if ctxErr := runCtx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if out.exceeded {
			return "", fmt.Errorf("%w: command stdout exceeds %d bytes", errDocumentExtractionLimit, maxDocumentOutputBytes)
		}
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return "", fmt.Errorf("%s", firstLine(msg))
		}
		return "", err
	}
	if ctxErr := runCtx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if out.exceeded {
		return "", fmt.Errorf("%w: command stdout exceeds %d bytes", errDocumentExtractionLimit, maxDocumentOutputBytes)
	}
	return strings.TrimSpace(out.String()), nil
}

// readBoundedRegularFile checks the on-disk size before allocation, then
// rechecks actual bytes through a limit+1 reader to close stat/read races.
func readBoundedRegularFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("bounded read requires a regular file: %q", path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%w: file %q exceeds %d bytes", errDocumentExtractionLimit, path, maxBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: file %q exceeded %d bytes while reading", errDocumentExtractionLimit, path, maxBytes)
	}
	return data, nil
}

// validateOCRImage reads only the encoded image header. It rejects dimensions
// that would expand a small hostile payload into excessive decoder/GPU memory.
func validateOCRImage(data []byte) error {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		width, height, fallbackErr := extendedImageDimensions(data)
		if fallbackErr != nil {
			return fmt.Errorf("이미지 헤더 파싱 실패: %w", fallbackErr)
		}
		config.Width, config.Height = width, height
	}
	if config.Width <= 0 || config.Height <= 0 {
		return errors.New("이미지 크기가 올바르지 않습니다")
	}
	if config.Width > maxOCRImageDimension || config.Height > maxOCRImageDimension {
		return fmt.Errorf("%w: image dimensions %dx%d exceed %d", errDocumentExtractionLimit, config.Width, config.Height, maxOCRImageDimension)
	}
	pixels := uint64(config.Width) * uint64(config.Height)
	if pixels > maxOCRImagePixels {
		return fmt.Errorf("%w: image has %d pixels, limit %d", errDocumentExtractionLimit, pixels, maxOCRImagePixels)
	}
	return nil
}

// extendedImageDimensions preserves the existing BMP/WebP/TIFF OCR surface
// without adding a decoder dependency. Only fixed-size header/IFD fields are
// read; pixel data is never materialized.
func extendedImageDimensions(data []byte) (int, int, error) {
	switch {
	case len(data) >= 26 && string(data[:2]) == "BM":
		return bmpDimensions(data)
	case len(data) >= 30 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return webpDimensions(data)
	case len(data) >= 8 && (string(data[:4]) == "II*\x00" || string(data[:4]) == "MM\x00*"):
		return tiffDimensions(data)
	default:
		return 0, 0, errors.New("unsupported image format")
	}
}

func bmpDimensions(data []byte) (int, int, error) {
	dibSize := binary.LittleEndian.Uint32(data[14:18])
	if dibSize == 12 {
		if len(data) < 22 {
			return 0, 0, errors.New("truncated BMP core header")
		}
		return int(binary.LittleEndian.Uint16(data[18:20])), int(binary.LittleEndian.Uint16(data[20:22])), nil
	}
	if dibSize < 40 || len(data) < 26 {
		return 0, 0, errors.New("unsupported BMP header")
	}
	width := int32(binary.LittleEndian.Uint32(data[18:22]))
	height := int32(binary.LittleEndian.Uint32(data[22:26]))
	if width <= 0 || height == 0 || height == -1<<31 {
		return 0, 0, errors.New("invalid BMP dimensions")
	}
	if height < 0 {
		height = -height
	}
	return int(width), int(height), nil
}

func webpDimensions(data []byte) (int, int, error) {
	switch string(data[12:16]) {
	case "VP8X":
		width := 1 + int(data[24]) + int(data[25])<<8 + int(data[26])<<16
		height := 1 + int(data[27]) + int(data[28])<<8 + int(data[29])<<16
		return width, height, nil
	case "VP8L":
		if len(data) < 25 || data[20] != 0x2f {
			return 0, 0, errors.New("invalid lossless WebP header")
		}
		width := 1 + int(data[21]) + int(data[22]&0x3f)<<8
		height := 1 + int(data[22]>>6) + int(data[23])<<2 + int(data[24]&0x0f)<<10
		return width, height, nil
	case "VP8 ":
		if len(data) < 30 || !bytes.Equal(data[23:26], []byte{0x9d, 0x01, 0x2a}) {
			return 0, 0, errors.New("invalid lossy WebP header")
		}
		width := int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff)
		height := int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff)
		return width, height, nil
	default:
		return 0, 0, errors.New("unsupported WebP chunk")
	}
}

func tiffDimensions(data []byte) (int, int, error) {
	var order binary.ByteOrder
	if string(data[:2]) == "II" {
		order = binary.LittleEndian
	} else {
		order = binary.BigEndian
	}
	ifdOffset := uint64(order.Uint32(data[4:8]))
	if ifdOffset+2 > uint64(len(data)) {
		return 0, 0, errors.New("TIFF IFD out of range")
	}
	count := uint64(order.Uint16(data[ifdOffset : ifdOffset+2]))
	if count > 4096 || ifdOffset+2+count*12+4 > uint64(len(data)) {
		return 0, 0, errors.New("TIFF IFD too large")
	}
	var width, height uint32
	for i := uint64(0); i < count; i++ {
		offset := ifdOffset + 2 + i*12
		entry := data[offset : offset+12]
		tag := order.Uint16(entry[:2])
		if tag != 256 && tag != 257 {
			continue
		}
		if order.Uint32(entry[4:8]) != 1 {
			return 0, 0, errors.New("unsupported TIFF dimension count")
		}
		var value uint32
		switch order.Uint16(entry[2:4]) {
		case 3:
			value = uint32(order.Uint16(entry[8:10]))
		case 4:
			value = order.Uint32(entry[8:12])
		default:
			return 0, 0, errors.New("unsupported TIFF dimension type")
		}
		if tag == 256 {
			width = value
		} else {
			height = value
		}
	}
	if width == 0 || height == 0 {
		return 0, 0, errors.New("TIFF dimensions missing")
	}
	nextOffsetPos := ifdOffset + 2 + count*12
	if order.Uint32(data[nextOffsetPos:nextOffsetPos+4]) != 0 {
		// Tesseract walks every IFD in a multipage TIFF. Rejecting the format here
		// prevents a harmless first page from hiding oversized or unbounded later
		// pages; single-page TIFF OCR remains supported.
		return 0, 0, fmt.Errorf("%w: multipage TIFF OCR is not supported", errDocumentExtractionLimit)
	}
	return int(width), int(height), nil
}

func preflightConverterInput(data []byte, ext string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		return preflightConverterZIP(zr)
	}
	if isZipConverterInput(ext) || hasZIPMagic(data) {
		return fmt.Errorf("converter archive invalid: %w", err)
	}
	return nil
}

func hasZIPMagic(data []byte) bool {
	if len(data) < 4 || data[0] != 'P' || data[1] != 'K' {
		return false
	}
	switch string(data[2:4]) {
	case "\x03\x04", "\x05\x06", "\x07\x08":
		return true
	default:
		return false
	}
}

func preflightConverterZIP(zr *zip.Reader) error {
	if err := validateConverterArchiveMetadata(zr); err != nil {
		return err
	}
	remaining := int64(maxConverterArchiveTotal)
	for _, entry := range zr.File {
		limit := min(int64(maxConverterArchivePartBytes), remaining)
		rc, err := entry.Open()
		if err != nil {
			return fmt.Errorf("converter archive part %q: %w", entry.Name, err)
		}
		lr := &io.LimitedReader{R: rc, N: limit + 1}
		_, copyErr := io.Copy(io.Discard, lr)
		closeErr := rc.Close()
		if copyErr != nil {
			return fmt.Errorf("converter archive part %q: %w", entry.Name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("converter archive part %q: %w", entry.Name, closeErr)
		}
		consumed := limit + 1 - lr.N
		if consumed > limit {
			return fmt.Errorf("%w: converter archive part %q exceeds runtime budget", errDocumentExtractionLimit, entry.Name)
		}
		remaining -= consumed
	}
	return nil
}

func validateConverterArchiveMetadata(zr *zip.Reader) error {
	if len(zr.File) > maxConverterArchiveParts {
		return fmt.Errorf("%w: converter archive has more than %d entries", errDocumentExtractionLimit, maxConverterArchiveParts)
	}
	seen := make(map[string]struct{}, len(zr.File))
	var total uint64
	for _, entry := range zr.File {
		if _, duplicate := seen[entry.Name]; duplicate {
			return fmt.Errorf("converter archive has duplicate part %q", entry.Name)
		}
		seen[entry.Name] = struct{}{}
		if entry.UncompressedSize64 > maxConverterArchivePartBytes {
			return fmt.Errorf("%w: converter archive part %q exceeds %d bytes", errDocumentExtractionLimit, entry.Name, maxConverterArchivePartBytes)
		}
		if entry.UncompressedSize64 > maxConverterArchiveTotal-total {
			return fmt.Errorf("%w: converter archive exceeds %d uncompressed bytes", errDocumentExtractionLimit, maxConverterArchiveTotal)
		}
		total += entry.UncompressedSize64
	}
	return nil
}

func joinTextWithLimit(parts []string, separator string, maxBytes int) (string, error) {
	out := newLimitedBuffer(maxBytes, false)
	for i, part := range parts {
		if i > 0 {
			if _, err := out.WriteString(separator); err != nil {
				return "", err
			}
		}
		if _, err := out.WriteString(part); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}
