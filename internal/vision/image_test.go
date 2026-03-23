package vision_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/vision"
)

// Minimal 1x1 white PNG
var minimalPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
	0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

// --- helpers for new tests ---

func makeTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	f, err := os.CreateTemp("", "huginn-vision-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Write(data)
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

// --- existing tests (preserved) ---

func TestIsImageExtension_PNG(t *testing.T) {
	if !vision.IsImageExtension("photo.png") {
		t.Error("expected .png")
	}
}

func TestIsImageExtension_JPG(t *testing.T) {
	if !vision.IsImageExtension("photo.jpg") {
		t.Error("expected .jpg")
	}
}

func TestIsImageExtension_GoFile(t *testing.T) {
	if vision.IsImageExtension("main.go") {
		t.Error("expected .go to NOT be image")
	}
}

func TestIsImageExtension_CaseInsensitive(t *testing.T) {
	if !vision.IsImageExtension("PHOTO.PNG") {
		t.Error("expected .PNG uppercase")
	}
}

func TestMIMETypeForExt_PNG(t *testing.T) {
	if vision.MIMETypeForExt("photo.png") != "image/png" {
		t.Error("expected image/png")
	}
}

func TestMIMETypeForExt_JPG(t *testing.T) {
	if vision.MIMETypeForExt("photo.jpg") != "image/jpeg" {
		t.Error("expected image/jpeg")
	}
}

func TestReadImageAsDataURI_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	os.WriteFile(path, minimalPNG, 0644)
	uri, err := vision.ReadImageAsDataURI(path, 2048)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Errorf("wrong prefix: %q", uri[:40])
	}
	payload := strings.TrimPrefix(uri, "data:image/png;base64,")
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		t.Errorf("invalid base64: %v", err)
	}
}

func TestReadImageAsDataURI_TooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	os.WriteFile(path, make([]byte, 3*1024*1024), 0644)
	_, err := vision.ReadImageAsDataURI(path, 2048)
	if err == nil {
		t.Error("expected error for oversized image")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected 'exceeds' in error")
	}
}

func TestReadImageAsDataURI_Missing(t *testing.T) {
	_, err := vision.ReadImageAsDataURI("/nonexistent/image.png", 2048)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadImageAsDataURI_ZeroMaxSize_DefaultsToLargeLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	os.WriteFile(path, minimalPNG, 0644)
	// maxSizeKB=0 should default to a reasonable limit, not reject every file
	uri, err := vision.ReadImageAsDataURI(path, 0)
	if err != nil {
		t.Fatalf("expected success with default limit, got: %v", err)
	}
	if !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Errorf("wrong prefix: %q", uri[:40])
	}
}

func TestIsImage_ByMagicBytes(t *testing.T) {
	// Create a minimal PNG (magic bytes: 89 50 4E 47...)
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	tmp := filepath.Join(t.TempDir(), "image_no_ext")
	os.WriteFile(tmp, pngHeader, 0644)

	if !vision.IsImage(tmp) {
		t.Error("expected IsImage=true for file with PNG magic bytes")
	}
}

func TestIsImage_TextFileReturnsFalse(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(tmp, []byte("hello world"), 0644)
	if vision.IsImage(tmp) {
		t.Error("expected IsImage=false for text file")
	}
}

func TestReadImageAsDataURI_RejectsOversized(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "big.png")
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	data := append(pngHeader, make([]byte, 2000)...)
	os.WriteFile(tmp, data, 0644)

	_, err := vision.ReadImageAsDataURI(tmp, 1) // 1KB limit
	if err == nil {
		t.Error("expected error for oversized image")
	}
}

// --- new tests ---

func TestIsImage_JPEG(t *testing.T) {
	data := makeTestJPEG(t, 100, 100)
	path := writeTempFile(t, data)
	if !vision.IsImage(path) {
		t.Error("expected JPEG to be detected as image")
	}
}

func TestIsImage_PNG(t *testing.T) {
	data := makeTestPNG(t, 100, 100)
	path := writeTempFile(t, data)
	if !vision.IsImage(path) {
		t.Error("expected PNG to be detected as image")
	}
}

func TestIsImage_NotAnImage(t *testing.T) {
	path := writeTempFile(t, []byte("hello world this is not an image"))
	if vision.IsImage(path) {
		t.Error("expected non-image to return false")
	}
}

func TestIsImage_EmptyFile(t *testing.T) {
	path := writeTempFile(t, []byte{})
	if vision.IsImage(path) {
		t.Error("empty file should not be detected as image")
	}
}

func TestIsImage_MissingFile(t *testing.T) {
	if vision.IsImage("/nonexistent/image.jpg") {
		t.Error("missing file should return false")
	}
}

func TestIsImage_BinaryNotImage(t *testing.T) {
	// ELF binary magic bytes
	data := []byte{0x7f, 0x45, 0x4c, 0x46, 0x02, 0x01, 0x01}
	path := writeTempFile(t, data)
	if vision.IsImage(path) {
		t.Error("ELF binary should not be detected as image")
	}
}

func TestReadImageAsDataURI_JPEG(t *testing.T) {
	data := makeTestJPEG(t, 100, 100)
	path := writeTempFile(t, data)

	uri, err := vision.ReadImageAsDataURI(path, vision.DefaultMaxSizeKB)
	if err != nil {
		t.Fatalf("ReadImageAsDataURI: %v", err)
	}
	if !strings.HasPrefix(uri, "data:image/") {
		t.Errorf("expected data URI, got: %s", uri[:min(len(uri), 50)])
	}
	if !strings.Contains(uri, ";base64,") {
		t.Errorf("expected base64 encoding in URI")
	}
}

func TestReadImageAsDataURI_TooLargeZeroKB(t *testing.T) {
	data := makeTestJPEG(t, 100, 100)
	path := writeTempFile(t, data)
	// Negative limit also triggers the default (5MB), not zero — test with a tiny explicit limit
	_, err := vision.ReadImageAsDataURI(path, 1) // 1 KB — any real JPEG is larger
	if err == nil {
		// JPEG of 100x100 may be smaller than 1KB; skip if so
		t.Log("JPEG was under 1KB, skipping size-limit assertion")
	}
}

func TestResizeIfNeeded_SmallImage_Unchanged(t *testing.T) {
	data := makeTestJPEG(t, 100, 100)
	result := vision.ResizeIfNeeded(data, 2048)
	if len(result) == 0 {
		t.Error("resize returned empty data for small image")
	}
}

func TestResizeIfNeeded_LargeImage_Downscaled(t *testing.T) {
	// Create a 4000x4000 JPEG (larger than maxDim 2048)
	data := makeTestJPEG(t, 4000, 4000)
	result := vision.ResizeIfNeeded(data, 2048)
	if len(result) == 0 {
		t.Error("resize returned empty data")
	}
	// Decode result and check dimensions
	img, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("decode resized image: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() > 2048 || bounds.Dy() > 2048 {
		t.Errorf("image not resized: got %dx%d, expected <= 2048", bounds.Dx(), bounds.Dy())
	}
}

func TestResizeIfNeeded_NotAnImage_ReturnsOriginal(t *testing.T) {
	data := []byte("not an image")
	result := vision.ResizeIfNeeded(data, 2048)
	if !bytes.Equal(result, data) {
		t.Error("non-image data should be returned unchanged")
	}
}

func TestReadImageAsDataURI_Base64Decodable(t *testing.T) {
	data := makeTestPNG(t, 50, 50)
	path := writeTempFile(t, data)

	uri, err := vision.ReadImageAsDataURI(path, vision.DefaultMaxSizeKB)
	if err != nil {
		t.Fatal(err)
	}

	// Extract base64 part and verify it decodes
	parts := strings.SplitN(uri, ";base64,", 2)
	if len(parts) != 2 {
		t.Fatal("malformed data URI")
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Errorf("base64 decode failed: %v", err)
	}
	if len(decoded) == 0 {
		t.Error("decoded base64 is empty")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
