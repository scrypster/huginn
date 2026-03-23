package vision

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadImageAsDataURI_SizeLimit_ExactLimit verifies behavior at exact size limit.
func TestReadImageAsDataURI_SizeLimit_ExactLimit(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "exact.png")

	// Create a PNG at exactly the limit (5120 KB * 1024 bytes = 5,242,880 bytes)
	limitBytes := 5120 * 1024
	content := make([]byte, limitBytes)
	// Write PNG magic bytes at the start
	copy(content, []byte{137, 80, 78, 71, 13, 10, 26, 10}) // PNG magic
	if err := os.WriteFile(imgPath, content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should succeed at exact limit
	uri, err := ReadImageAsDataURI(imgPath, -1) // -1 uses default 5120 KB
	if err != nil {
		t.Errorf("ReadImageAsDataURI at exact limit should succeed, got error: %v", err)
	}
	if uri == "" {
		t.Error("ReadImageAsDataURI should return non-empty data URI")
	}
}

// TestReadImageAsDataURI_SizeLimit_OneByteOver verifies rejection when exactly one byte over.
func TestReadImageAsDataURI_SizeLimit_OneByteOver(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "over.png")

	// Create PNG one byte over the limit
	limitBytes := 5120 * 1024
	content := make([]byte, limitBytes+1)
	copy(content, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	if err := os.WriteFile(imgPath, content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	uri, err := ReadImageAsDataURI(imgPath, -1)
	if err == nil {
		t.Error("ReadImageAsDataURI one byte over limit should error")
	}
	if uri != "" {
		t.Errorf("ReadImageAsDataURI should return empty string on error, got %q", uri)
	}
}

// TestReadImageAsDataURI_CustomLimit verifies custom size limit works.
func TestReadImageAsDataURI_CustomLimit(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "custom.png")

	// Create 100 KB PNG
	content := make([]byte, 100*1024)
	copy(content, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	if err := os.WriteFile(imgPath, content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should succeed with 100 KB limit
	uri, err := ReadImageAsDataURI(imgPath, 100)
	if err != nil {
		t.Errorf("ReadImageAsDataURI with 100 KB limit for 100 KB file should succeed: %v", err)
	}
	if uri == "" {
		t.Error("ReadImageAsDataURI should return non-empty data URI")
	}

	// Should fail with 99 KB limit
	uri, err = ReadImageAsDataURI(imgPath, 99)
	if err == nil {
		t.Error("ReadImageAsDataURI with 99 KB limit for 100 KB file should fail")
	}
}

// TestReadImageAsDataURI_ZeroLimit uses default when limit is zero.
func TestReadImageAsDataURI_ZeroLimit(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "zero.png")

	// Create small PNG
	content := make([]byte, 1024) // 1 KB
	copy(content, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	if err := os.WriteFile(imgPath, content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Limit 0 should use default (5120 KB), so 1 KB should pass
	uri, err := ReadImageAsDataURI(imgPath, 0)
	if err != nil {
		t.Errorf("ReadImageAsDataURI with limit 0 should use default: %v", err)
	}
	if uri == "" {
		t.Error("ReadImageAsDataURI should return non-empty data URI")
	}
}

// TestMIMETypeForExt_UnknownExt returns application/octet-stream for unknown extensions.
func TestMIMETypeForExt_UnknownExt(t *testing.T) {
	mime := MIMETypeForExt("image.xyz")
	if mime != "application/octet-stream" {
		t.Errorf("expected application/octet-stream for unknown extension, got %q", mime)
	}
}

// TestMIMETypeForExt_CaseInsensitive verifies MIME type lookup is case-insensitive.
func TestMIMETypeForExt_CaseInsensitive(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"image.PNG", "image/png"},
		{"image.Png", "image/png"},
		{"image.pNG", "image/png"},
		{"image.JPG", "image/jpeg"},
		{"image.Jpg", "image/jpeg"},
		{"image.JPEG", "image/jpeg"},
	}

	for _, tt := range tests {
		got := MIMETypeForExt(tt.filename)
		if got != tt.expected {
			t.Errorf("MIMETypeForExt(%q): expected %q, got %q", tt.filename, tt.expected, got)
		}
	}
}

// TestIsImage_EmptyFile detects empty file as non-image.
func TestIsImage_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "empty.bin")
	if err := os.WriteFile(imgPath, []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if IsImage(imgPath) {
		t.Error("IsImage should return false for empty file")
	}
}

// TestIsImage_NonImage detects text file as non-image.
func TestIsImage_TextFile(t *testing.T) {
	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "text.txt")
	if err := os.WriteFile(txtPath, []byte("This is plain text"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if IsImage(txtPath) {
		t.Error("IsImage should return false for text file")
	}
}

// TestIsImage_NonExistent returns false for non-existent files.
func TestIsImage_NonExistent(t *testing.T) {
	if IsImage("/nonexistent/image.png") {
		t.Error("IsImage should return false for non-existent file")
	}
}

// TestIsImage_WithMagicBytes detects images by magic bytes, not extension.
func TestIsImage_MagicBytesOverExtension(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file with PNG magic bytes but .txt extension
	pngPath := filepath.Join(tmpDir, "notanimage.txt")
	pngMagic := []byte{137, 80, 78, 71, 13, 10, 26, 10, 1, 2, 3, 4}
	if err := os.WriteFile(pngPath, pngMagic, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// IsImage should detect it as image based on magic bytes
	if !IsImage(pngPath) {
		t.Error("IsImage should detect PNG by magic bytes even with wrong extension")
	}
}

// TestIsImageExtension_KnownExtensions verifies all known extensions.
func TestIsImageExtension_AllKnown(t *testing.T) {
	knownExts := []string{"image.png", "image.jpg", "image.jpeg", "image.webp", "image.gif"}
	for _, name := range knownExts {
		if !IsImageExtension(name) {
			t.Errorf("IsImageExtension should return true for %q", name)
		}
	}
}

// TestIsImageExtension_UnknownExtension returns false for unknown extensions.
func TestIsImageExtension_Unknown(t *testing.T) {
	if IsImageExtension("image.doc") {
		t.Error("IsImageExtension should return false for .doc")
	}
	if IsImageExtension("image.pdf") {
		t.Error("IsImageExtension should return false for .pdf")
	}
}

// TestIsImageExtension_NoExtension returns false for files without extension.
func TestIsImageExtension_NoExt(t *testing.T) {
	if IsImageExtension("image") {
		t.Error("IsImageExtension should return false for file without extension")
	}
}
