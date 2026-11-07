package cli_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/cli"
)

// stub is a Bield whose Docker and files are made up, since those need a
// machine behind them, and that records what it was asked.
type stub struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	uploaded []byte
	written  []byte
}

func (s *stub) saw() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{}
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encode the stub's answer: %v", err)
		}
	}
	mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, api.Snapshot{Devices: []api.Device{
			{ID: "nas1", Name: "NAS", HostName: "nas", DNSName: "nas.example.ts.net", Docker: true},
			{ID: "pi1", Name: "Pi", HostName: "pi", DNSName: "pi.example.ts.net"},
		}})
	})
	mux.HandleFunc("GET /api/devices/nas1/docker/containers", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, api.DockerContainerList{Containers: []api.DockerContainerRow{
			{ID: "aaaabbbbcccc", Name: "web", Image: "nginx", State: api.ContainerStateRunning, Status: "Up 2 hours"},
			{ID: "ddddeeeeffff", Name: "old", Image: "busybox", State: api.ContainerStateExited, Status: "Exited (0)"},
		}})
	})
	mux.HandleFunc("POST /api/devices/nas1/docker/containers/{cid}/{action}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, fmt.Sprintf("%s %s ua=%s csrf=%s", r.Method, r.URL.Path, r.UserAgent(), r.Header.Get(api.CSRFHeader)))
		s.mu.Unlock()
		reply(w, api.DockerActionResult{Changed: true})
	})
	mux.HandleFunc("GET /api/devices/nas1/docker/containers/{cid}/logs", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, "GET logs?"+r.URL.RawQuery)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: start\ndata: {\"tty\":false,\"running\":true}\n\n")
		fmt.Fprint(w, `event: lines`+"\n"+`data: [{"s":1,"t":"2026-11-07T10:00:00Z","m":"hello"},{"s":2,"t":"2026-11-07T10:00:01Z","m":"oops"}]`+"\n\n")
		fmt.Fprint(w, "event: end\ndata: {\"reason\":\"End of log\"}\n\n")
	})
	mux.HandleFunc("GET /api/devices/nas1/files/list", func(w http.ResponseWriter, r *http.Request) {
		reply(w, api.FileListing{Path: "/srv", Home: "/home/bield", Entries: []api.FileEntry{
			{Name: "b.txt", Type: api.FileTypeFile, Size: 2048, Mode: "-rw-r--r--", Owner: "root", Group: "root"},
			{Name: "data", Type: api.FileTypeDir, Mode: "drwxr-xr-x", Owner: "root", Group: "root"},
			{Name: "link", Type: api.FileTypeSymlink, Target: "b.txt", Mode: "lrwxrwxrwx", Owner: "root", Group: "root"},
		}})
	})
	mux.HandleFunc("GET /api/devices/nas1/files/raw", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "contents of %s\n", r.URL.Query().Get("path")) //nolint:gosec // G705: a test server answering plain text.
	})
	mux.HandleFunc("PUT /api/devices/nas1/files/upload", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.uploaded = body
		s.requests = append(s.requests, "PUT upload?"+r.URL.Query().Encode())
		s.mu.Unlock()
		q := r.URL.Query()
		reply(w, api.FileUpload{Path: q.Get("dir") + "/" + q.Get("name"), Name: q.Get("name"), Size: int64(len(body))})
	})
	mux.HandleFunc("PUT /api/devices/nas1/files/write", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.written = body
		s.requests = append(s.requests, "PUT write?"+r.URL.Query().Encode()+fmt.Sprintf(" length=%d", r.ContentLength))
		s.mu.Unlock()
		reply(w, api.FileEntry{Path: r.URL.Query().Get("path"), Size: int64(len(body))})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *stub) run(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	var stdout, stderr syncBuffer
	e := cli.Env{
		Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr, Version: "9.9.9",
		Getenv: func(k string) string { return map[string]string{"BIELD_URL": s.URL}[k] },
	}
	code := cli.Run(t.Context(), e, args)
	return result{stdout.String(), stderr.String(), code}
}

func TestDocker(t *testing.T) {
	t.Parallel()
	s := newStub(t)

	r := s.run(t, "", "docker", "ps")
	r.want(t, 0, "NAS     web", "")
	if strings.Contains(r.stdout, "old") {
		t.Errorf("docker ps = %q, want only running containers", r.stdout)
	}
	s.run(t, "", "docker", "ps", "-a").want(t, 0, "old", "")
	// A device with no Docker is left out when none is named.
	if r := s.run(t, "", "docker", "ps", "--json"); !strings.Contains(r.stdout, `"deviceName": "NAS"`) || strings.Contains(r.stdout, "Pi") {
		t.Errorf("docker ps --json = %q", r.stdout)
	}

	// Stopping and restarting need --yes and do nothing without it. Starting doesn't.
	s.run(t, "", "docker", "restart", "nas", "web").want(t, cli.ExitUsage, "", "add --yes")
	s.run(t, "", "docker", "stop", "nas", "web").want(t, cli.ExitUsage, "", "add --yes")
	if got := s.saw(); len(got) != 0 {
		t.Fatalf("requests before --yes = %q, want none", got)
	}
	s.run(t, "", "docker", "restart", "nas", "web", "--yes").want(t, 0, "Restarted web on NAS", "")
	s.run(t, "", "docker", "start", "nas", "aaaab").want(t, 0, "Started web on NAS", "")
	s.run(t, "", "docker", "start", "nas", "nope").want(t, cli.ExitNotFound, "", `no container is called "nope"`)
	got := s.saw()
	want := []string{
		"POST /api/devices/nas1/docker/containers/aaaabbbbcccc/restart ua=bield-cli/9.9.9 csrf=1",
		"POST /api/devices/nas1/docker/containers/aaaabbbbcccc/start ua=bield-cli/9.9.9 csrf=1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests = %q, want %q", got, want)
	}

	// Output lines go to stdout and error lines to stderr, like docker logs.
	r = s.run(t, "", "docker", "logs", "nas", "web", "-n", "50", "--since", "10m", "-t")
	r.want(t, 0, "2026-11-07T10:00:00Z hello", "2026-11-07T10:00:01Z oops")
	if strings.Contains(r.stdout, "oops") {
		t.Errorf("docker logs put error output on stdout: %q", r.stdout)
	}
	if last := s.saw()[len(s.saw())-1]; last != "GET logs?follow=0&since=10m&tail=50" {
		t.Errorf("logs request = %q", last)
	}
	r = s.run(t, "", "docker", "logs", "nas", "web", "--json", "-f")
	if lines := strings.Split(strings.TrimSpace(r.stdout), "\n"); len(lines) != 2 || !strings.Contains(lines[1], `"m":"oops"`) {
		t.Errorf("docker logs --json = %q, want a line of JSON for each log line", r.stdout)
	}
	if last := s.saw()[len(s.saw())-1]; last != "GET logs?tail=100" {
		t.Errorf("following logs request = %q, want the default tail of 100 and follow on", last)
	}
}

// It changes the working directory, which a parallel test can't.
func TestFiles(t *testing.T) {
	s := newStub(t)
	dir := t.TempDir()
	t.Chdir(dir)

	r := s.run(t, "", "files", "ls", "nas", "/srv")
	r.want(t, 0, "NAS:/srv", "")
	// Folders first, then by name, and a link shows where it goes.
	for _, want := range []string{"data/", "b.txt", "link -> b.txt"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("files ls = %q, want %q", r.stdout, want)
		}
	}
	if strings.Index(r.stdout, "data/") > strings.Index(r.stdout, "b.txt") {
		t.Errorf("files ls = %q, want folders first", r.stdout)
	}
	s.run(t, "", "files", "cat", "nas", "/etc/hosts").want(t, 0, "contents of /etc/hosts\n", "")

	// A download won't replace a file unless told to.
	s.run(t, "", "files", "get", "nas", "/srv/b.txt").want(t, 0, "Saved b.txt", "")
	if b, err := os.ReadFile(filepath.Join(dir, "b.txt")); err != nil || string(b) != "contents of /srv/b.txt\n" { //nolint:gosec // G304: the test's own temporary folder.
		t.Errorf("downloaded file = %q, %v", b, err)
	}
	s.run(t, "", "files", "get", "nas", "/srv/b.txt").want(t, cli.ExitFailed, "", "b.txt is already there; say --overwrite")
	s.run(t, "", "files", "get", "nas", "/srv/b.txt", "--overwrite").want(t, 0, "Saved b.txt", "")

	if err := os.WriteFile("up.txt", []byte("upload me"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.run(t, "", "files", "put", "nas", "up.txt", "/srv").want(t, 0, "Uploaded up.txt (9 B) to NAS:/srv/up.txt", "")
	s.run(t, "from stdin", "files", "put", "nas", "-", "/srv", "--name", "in.txt", "--rename").want(t, 0, "Uploaded in.txt", "")
	s.run(t, "", "files", "put", "nas", "up.txt", "/srv", "--overwrite", "--rename").want(t, cli.ExitUsage, "", "can't both be given")
	s.run(t, "", "files", "put", "nas", dir).want(t, cli.ExitFailed, "", "is a folder")
	if string(s.uploaded) != "from stdin" {
		t.Errorf("uploaded = %q, want the last upload's text", s.uploaded)
	}

	// Writing replaces a file, so it needs --yes, and sends its length.
	s.run(t, "new text\n", "files", "write", "nas", "/etc/motd").want(t, cli.ExitUsage, "", "add --yes")
	s.run(t, "new text\n", "files", "write", "nas", "/etc/motd", "--yes", "--create").want(t, 0, "Wrote /etc/motd (9 B)", "")
	if string(s.written) != "new text\n" {
		t.Errorf("written = %q", s.written)
	}
	saw := strings.Join(s.saw(), "\n")
	for _, want := range []string{
		"PUT upload?conflict=rename&dir=%2Fsrv&name=in.txt",
		"PUT upload?dir=%2Fsrv&name=up.txt",
		"PUT write?create=1&path=%2Fetc%2Fmotd length=9",
	} {
		if !strings.Contains(saw, want) {
			t.Errorf("requests = %q, want %q among them", saw, want)
		}
	}
}

func TestOldBield(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			_, _ = io.WriteString(w, `{"status":"ok","version":"v0.1.0"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	}))
	t.Cleanup(srv.Close)
	s := &stub{Server: srv}
	s.run(t, "", "devices").want(t, cli.ExitFailed, "", "this Bield (v0.1.0) is older than this bield command")
}
