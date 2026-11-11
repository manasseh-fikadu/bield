package editor

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// releaseJSON is the code-server release Bield installs, which `make
// code-server` writes from GitHub's list of releases.
//
//go:embed release.json
var releaseJSON []byte

// A Release is a version of code-server and its builds.
type Release struct {
	Version string  `json:"version"`
	Assets  []Asset `json:"assets"`
}

// An Asset is code-server's build for one system: a tarball, the SHA-256 of
// which Bield checks before it unpacks it on the machine.
type Asset struct {
	// OS is "linux" or "macos", and Arch "amd64" or "arm64", as code-server
	// names its builds.
	OS   string `json:"os"`
	Arch string `json:"arch"`
	URL  string `json:"url"`
	// SHA256 is the tarball's SHA-256, in hex.
	SHA256 string `json:"sha256"`
	// Size is the tarball's size in bytes.
	Size int64 `json:"size"`
	// Dir is the folder the tarball holds everything in.
	Dir string `json:"dir"`
}

// Unpacked is about how much disk code-server takes once it's unpacked, in
// bytes. Measured on 4.145.2 for Linux on amd64, which took 699 MB.
const Unpacked = 750 << 20

// Pinned returns the release Bield installs.
func Pinned() Release { return pinned() }

// pinned reads the release when it's first asked for, so a broken file
// doesn't stop the command that writes a new one.
var pinned = sync.OnceValue(func() Release { return mustRelease(releaseJSON) })

func mustRelease(b []byte) Release {
	var r Release
	if err := json.Unmarshal(b, &r); err != nil {
		panic(fmt.Sprintf("editor: read release.json: %v", err))
	}
	if r.Version == "" || len(r.Assets) == 0 {
		panic("editor: release.json names no version or no builds")
	}
	return r
}

// Asset returns the build for a machine that uname says is system (uname -s)
// on machine (uname -m), or false if code-server has none.
func (r Release) Asset(system, machine string) (Asset, bool) {
	var os, arch string
	switch system {
	case "Linux":
		os = "linux"
	case "Darwin":
		os = "macos"
	}
	switch machine {
	case "x86_64", "amd64":
		arch = "amd64"
	case "aarch64", "arm64":
		arch = "arm64"
	}
	for _, a := range r.Assets {
		if a.OS == os && a.Arch == arch {
			return a, true
		}
	}
	return Asset{}, false
}
