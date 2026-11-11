// Command gen writes internal/editor/release.json, the code-server release
// that Bield installs, from GitHub's list of code-server's releases. Run it
// with `make code-server`, which pins the latest release, or pass -version.
//
// GitHub keeps the SHA-256 of every file it serves for a release, so Bield
// pins each tarball's hash from there.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/editor"
)

const releases = "https://api.github.com/repos/coder/code-server/releases/"

// systems are the builds Bield installs: the systems its SSH reaches, and
// that code-server builds for.
var systems = []struct{ os, arch string }{
	{"linux", "amd64"}, {"linux", "arm64"}, {"macos", "amd64"}, {"macos", "arm64"},
}

func main() {
	version := flag.String("version", "", "pin this version, such as 4.145.2, instead of the latest")
	in := flag.String("in", "", "read GitHub's answer from this file instead of the web")
	out := flag.String("out", "internal/editor/release.json", "write the release here")
	flag.Parse()
	if err := run(*version, *in, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func run(version, in, out string) error {
	raw, err := read(version, in)
	if err != nil {
		return err
	}
	rel, err := pin(raw)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(rel, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil { //nolint:gosec // G306: a file in the repository.
		return err
	}
	fmt.Printf("pinned code-server %s\n", rel.Version)
	return nil
}

// pin returns the release that GitHub's answer describes.
func pin(raw []byte) (editor.Release, error) {
	var gh githubRelease
	if err := json.Unmarshal(raw, &gh); err != nil {
		return editor.Release{}, fmt.Errorf("read GitHub's answer: %w", err)
	}
	if gh.Draft || gh.Prerelease {
		return editor.Release{}, fmt.Errorf("%s is a draft or a prerelease", gh.TagName)
	}
	version := strings.TrimPrefix(gh.TagName, "v")
	if version == "" {
		return editor.Release{}, errors.New("GitHub's answer names no release")
	}
	rel := editor.Release{Version: version}
	for _, s := range systems {
		dir := fmt.Sprintf("code-server-%s-%s-%s", version, s.os, s.arch)
		found := false
		for _, a := range gh.Assets {
			if a.Name != dir+".tar.gz" {
				continue
			}
			sum, ok := strings.CutPrefix(a.Digest, "sha256:")
			if !ok || len(sum) != 64 {
				return editor.Release{}, fmt.Errorf("GitHub has no SHA-256 for %s", a.Name)
			}
			rel.Assets = append(rel.Assets, editor.Asset{
				OS: s.os, Arch: s.arch, URL: a.URL, SHA256: sum, Size: a.Size, Dir: dir,
			})
			found = true
		}
		if !found {
			return editor.Release{}, fmt.Errorf("code-server %s has no build for %s on %s", version, s.os, s.arch)
		}
	}
	return rel, nil
}

func read(version, path string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path) //nolint:gosec // G304: a path someone gave to a command they run.
	}
	url := releases + "latest"
	if version != "" {
		url = releases + "tags/v" + strings.TrimPrefix(version, "v")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch the release from GitHub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch the release from GitHub: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
