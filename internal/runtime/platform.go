package runtime

import (
	"os/exec"
	goruntime "runtime"
)

// Platform identifies the current OS, arch, and GPU capabilities.
type Platform struct {
	OS   string // "darwin", "linux", "windows"
	Arch string // "arm64", "amd64"
	CUDA bool   // true if nvidia-smi found on linux
}

// Detect returns the current platform.
func Detect() Platform {
	p := Platform{OS: goruntime.GOOS, Arch: goruntime.GOARCH}
	if p.OS == "linux" {
		if _, err := exec.LookPath("nvidia-smi"); err == nil {
			p.CUDA = true
		}
	}
	return p
}

// Key returns the manifest key for this platform, e.g. "linux-amd64-cuda".
func (p Platform) Key() string {
	key := p.OS + "-" + p.Arch
	if p.CUDA {
		key += "-cuda"
	}
	return key
}
