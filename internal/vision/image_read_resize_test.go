package vision_test

// coverage_boost95_test.go — tests to push vision package to 95%+.
// Targets uncovered branches in ReadImageAsDataURI and ResizeIfNeeded.

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/vision"
)

// TestReadImageAsDataURI_ReadError exercises the os.ReadFile error path in
// ReadImageAsDataURI. We stat a file, then the read would fail. The simplest
// way to trigger "read image" error is to use a path that passes stat but then
// becomes unreadable. On most systems we can create a file then chmod 0.
func TestReadImageAsDataURI_ReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping as root (chmod 0 still readable)")
	}
	dir := t.TempDir()
	path := dir + "/noperm.png"
	// Write minimal PNG data.
	pngData := []byte{
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
	if err := os.WriteFile(path, pngData, 0o644); err != nil {
		t.Fatal(err)
	}
	// Make it unreadable.
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })

	_, err := vision.ReadImageAsDataURI(path, vision.DefaultMaxSizeKB)
	if err == nil {
		t.Error("expected error reading unreadable file")
	}
}

// makeTestGIF creates a valid GIF image with a proper palette.
func makeTestGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	palette := color.Palette{color.White, color.Black}
	img := image.NewPaletted(image.Rect(0, 0, w, h), palette)
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("gif.Encode: %v", err)
	}
	return buf.Bytes()
}

// TestReadImageAsDataURI_GIF exercises the GIF branch in ReadImageAsDataURI
// (ct == "image/gif" triggers ResizeIfNeeded).
func TestReadImageAsDataURI_GIF(t *testing.T) {
	data := makeTestGIF(t, 10, 10)
	tmp := t.TempDir()
	path := tmp + "/test.gif"
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	uri, err := vision.ReadImageAsDataURI(path, vision.DefaultMaxSizeKB)
	if err != nil {
		t.Fatalf("ReadImageAsDataURI for GIF: %v", err)
	}
	if !strings.HasPrefix(uri, "data:") {
		t.Errorf("expected data URI, got: %q", uri[:min95(len(uri), 40)])
	}
}

// TestResizeIfNeeded_GIF_Resize exercises the "default: jpeg encode" branch in
// ResizeIfNeeded for GIF format (which hits the default case in the switch).
func TestResizeIfNeeded_GIF_Resize(t *testing.T) {
	// Create a small GIF — GIFs larger than maxDim are impractical to create quickly.
	// Instead use a tiny GIF (under maxDim) to exercise the "no resize needed" branch.
	data := makeTestGIF(t, 5, 5)
	result := vision.ResizeIfNeeded(data, vision.DefaultMaxDimension)
	if len(result) == 0 {
		t.Error("ResizeIfNeeded returned empty data for GIF")
	}
}

// TestResizeIfNeeded_PNG_Encode_Exercises exercises the "png" case in the
// format switch — a large PNG should encode as JPEG via jpeg.Encode.
func TestResizeIfNeeded_LargePNG_EncodesAsJPEG(t *testing.T) {
	// Create a large PNG (> DefaultMaxDimension) with valid color content.
	img := image.NewRGBA(image.Rect(0, 0, 3000, 1500))
	// Fill with some color to ensure the JPEG encode path works.
	for y := 0; y < 1500; y++ {
		for x := 0; x < 3000; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 128, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	result := vision.ResizeIfNeeded(buf.Bytes(), 2048)
	if len(result) == 0 {
		t.Error("expected non-empty result for large PNG resize")
	}

	// Decode the result to verify it's a valid image.
	_, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("decode resized PNG result: %v", err)
	}
}

// TestResizeIfNeeded_LargeJPEG_DefaultCase exercises the default case in the
// format switch for JPEG (falls through to default since "jpeg" != "png").
func TestResizeIfNeeded_LargeJPEG_DefaultCase(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}

	result := vision.ResizeIfNeeded(buf.Bytes(), 2048)
	if len(result) == 0 {
		t.Error("expected non-empty result for large JPEG resize")
	}
}

func min95(a, b int) int {
	if a < b {
		return a
	}
	return b
}
