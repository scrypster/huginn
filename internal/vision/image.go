package vision

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/gif"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultMaxSizeKB    = 20480 // 20 MB
	DefaultMaxDimension = 2048
	sniffLen            = 512 // bytes needed for magic-byte detection
)

var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// IsImage detects image files by magic bytes (first 512 bytes), not extension.
// Returns false if the file cannot be read.
func IsImage(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, sniffLen)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	mime := http.DetectContentType(buf[:n])
	return strings.HasPrefix(mime, "image/")
}

// IsImageExtension is kept for backward compatibility. Prefer IsImage.
// Returns true if the path has a common image extension.
func IsImageExtension(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := imageExtensions[ext]
	return ok
}

// MIMETypeForExt returns the MIME type for a known image extension,
// or "application/octet-stream" for unknown extensions.
func MIMETypeForExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if mime, ok := imageExtensions[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

// ReadImageAsDataURI reads the image at path and returns a data URI.
// maxSizeKB: reject files larger than this many KB. Pass DefaultMaxSizeKB for
// the default. A value <= 0 defaults to 5120 KB (5 MB).
// Returns error if file is too large, unreadable, or not an image.
func ReadImageAsDataURI(path string, maxSizeKB int) (string, error) {
	if maxSizeKB <= 0 {
		maxSizeKB = 5120 // default 5 MB
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("image file: %w", err)
	}
	limitBytes := int64(maxSizeKB) * 1024
	if info.Size() > limitBytes {
		return "", fmt.Errorf("image %q (%d KB) exceeds maximum allowed size of %d KB",
			filepath.Base(path), info.Size()/1024, maxSizeKB)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read image %q: %w", path, err)
	}
	// Secondary check after read to guard against TOCTOU (file grew between stat and read).
	if int64(len(data)) > limitBytes {
		return "", fmt.Errorf("image %q (%d KB) exceeds maximum allowed size of %d KB",
			filepath.Base(path), int64(len(data))/1024, maxSizeKB)
	}

	// Detect MIME type from magic bytes.
	ct := http.DetectContentType(data[:min(len(data), sniffLen)])
	if !strings.HasPrefix(ct, "image/") {
		// Fall back to extension-based detection for formats http.DetectContentType
		// may not recognize (e.g. some webp variants).
		ct = MIMETypeForExt(path)
		if ct == "application/octet-stream" {
			return "", fmt.Errorf("not a recognized image format")
		}
	}

	// Resize if dimensions exceed the default maximum.
	if ct == "image/jpeg" || ct == "image/png" || ct == "image/gif" {
		data = ResizeIfNeeded(data, DefaultMaxDimension)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", ct, encoded), nil
}

// ResizeIfNeeded downscales the image if either dimension exceeds maxDim.
// Returns the original data unchanged if resize fails or is not needed.
// Only processes JPEG/PNG/GIF (webp is returned as-is after decode failure).
func ResizeIfNeeded(data []byte, maxDim int) []byte {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data // can't decode, return original
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if w <= maxDim && h <= maxDim {
		return data // no resize needed
	}

	// Calculate new dimensions preserving aspect ratio.
	var newW, newH int
	if w > h {
		newW = maxDim
		newH = h * maxDim / w
	} else {
		newH = maxDim
		newW = w * maxDim / h
	}

	resized := resizeNearest(img, newW, newH)

	var buf bytes.Buffer
	switch format {
	case "png":
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85}); err != nil {
			return data
		}
	default: // jpeg, gif, and others — encode as JPEG
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85}); err != nil {
			return data
		}
	}
	return buf.Bytes()
}

// resizeNearest scales src to newW x newH using nearest-neighbor interpolation.
func resizeNearest(src image.Image, newW, newH int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			srcX := x * srcW / newW
			srcY := y * srcH / newH
			dst.Set(x, y, src.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}
	return dst
}
