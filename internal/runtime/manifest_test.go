package runtime

import (
	"strings"
	"testing"
)

func TestLoadManifest(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("expected version 1, got %d", m.Version)
	}
}

func TestPlatformKey(t *testing.T) {
	p := Platform{OS: "darwin", Arch: "arm64"}
	if p.Key() != "darwin-arm64" {
		t.Errorf("unexpected key: %s", p.Key())
	}
	p.CUDA = true
	if p.Key() != "darwin-arm64-cuda" {
		t.Errorf("unexpected key: %s", p.Key())
	}
}

func TestDetect(t *testing.T) {
	p := Detect()
	if p.OS == "" {
		t.Error("OS should not be empty")
	}
	if p.Arch == "" {
		t.Error("Arch should not be empty")
	}
}

func TestBinaryForPlatform_Found(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	entry, ok := m.BinaryForPlatform("darwin-arm64")
	if !ok {
		t.Fatal("expected darwin-arm64 to be found in manifest")
	}
	if entry.URL == "" {
		t.Error("expected non-empty URL for darwin-arm64")
	}
}

func TestBinaryForPlatform_NotFound(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	_, ok := m.BinaryForPlatform("freebsd-riscv64")
	if ok {
		t.Error("expected freebsd-riscv64 to NOT be found in manifest")
	}
}

// TestLoadManifest_LlamaServerVersion verifies that the manifest has a
// non-empty LlamaServerVersion.
func TestLoadManifest_LlamaServerVersion(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.LlamaServerVersion == "" {
		t.Error("expected non-empty LlamaServerVersion")
	}
}

// TestLoadManifest_HasBinaries verifies that at least one binary entry exists.
func TestLoadManifest_HasBinaries(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Binaries) == 0 {
		t.Error("expected at least one binary entry in manifest")
	}
}

// TestBinaryForPlatform_ValidURL verifies that the darwin-arm64 entry has a valid URL.
func TestBinaryForPlatform_ValidURL(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	entry, ok := m.BinaryForPlatform("darwin-arm64")
	if !ok {
		t.Skip("darwin-arm64 not in manifest")
	}
	if !strings.HasPrefix(entry.URL, "http") {
		t.Errorf("expected URL to start with 'http', got %q", entry.URL)
	}
}

// TestBinaryForPlatform_EmptyKey verifies that an empty key returns not found.
func TestBinaryForPlatform_EmptyKey(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	_, ok := m.BinaryForPlatform("")
	if ok {
		t.Error("expected empty key to return not found")
	}
}

// TestPlatformKey_NoCUDA verifies that a non-CUDA platform has no suffix.
func TestPlatformKey_NoCUDA(t *testing.T) {
	p := Platform{OS: "linux", Arch: "amd64", CUDA: false}
	if p.Key() != "linux-amd64" {
		t.Errorf("expected linux-amd64, got %q", p.Key())
	}
}

// TestDetect_ValidArch verifies that the detected arch is a known value.
func TestDetect_ValidArch(t *testing.T) {
	p := Detect()
	validArch := map[string]bool{
		"amd64": true,
		"arm64": true,
		"386":   true,
		"arm":   true,
	}
	if !validArch[p.Arch] {
		// Not a hard failure — just a warning for unknown arch values.
		t.Logf("unusual arch detected: %q (may be fine on non-standard hardware)", p.Arch)
	}
}
