// SPDX-License-Identifier: MIT

package agent_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/bield/agent"
)

func TestInstall(t *testing.T) {
	t.Parallel()
	script := agent.Install("https://bield.example.ts.net", "CODE234")
	for _, want := range []string{
		"\nSERVER_URL='https://bield.example.ts.net'\n",
		"\nCODE='CODE234'\n",
		`AGENT_VERSION="` + agent.Version + `"`,
		"\nBIELD_AGENT_EOF\n",
		"\nNAME=\"bield-agent\"\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("Install() doesn't contain %q", want)
		}
	}
	if strings.Contains(script, "__") {
		t.Error("Install() left a placeholder in the script")
	}

	// A URL can't break out of its quotes.
	script = agent.Install("https://x/'; rm -rf ~; '", "CODE234")
	if want := `SERVER_URL='https://x/'\''; rm -rf ~; '\'''`; !strings.Contains(script, want) {
		t.Errorf("Install() with a quote in the URL doesn't contain %s", want)
	}
	checkSyntax(t, script)
}

func TestUninstall(t *testing.T) {
	t.Parallel()
	script := agent.Uninstall()
	for _, want := range []string{"\nNAME=\"bield-agent\"\n", "/api/agent/enrollment"} {
		if !strings.Contains(script, want) {
			t.Errorf("Uninstall() doesn't contain %q", want)
		}
	}
	if strings.Contains(script, "__") {
		t.Error("Uninstall() left a placeholder in the script")
	}
	checkSyntax(t, script)
}

// checkSyntax fails the test if bash can't read script.
func checkSyntax(t *testing.T, script string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash to check the script's syntax")
	}
	cmd := exec.CommandContext(t.Context(), "bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("bash -n = %v: %s", err, out)
	}
}

func TestOlder(t *testing.T) {
	t.Parallel()
	if agent.Version == "" {
		t.Fatal("Version is empty")
	}
	for _, tt := range []struct {
		version string
		want    bool
	}{
		{agent.Version, false},
		{"0.9.9", true},
		{"1.2.0", true},
		{"1.10.0", false},
		{"99.0.0", false},
		{"", true},
		{"1.2", true},
		{"one.two.three", true},
		{"1.2.-1", true},
	} {
		if got := agent.Older(tt.version); got != tt.want {
			t.Errorf("Older(%q) = %v, want %v (the hub serves %s)", tt.version, got, tt.want, agent.Version)
		}
	}
}

// A hostname with a control character in it must not turn the heartbeat into
// invalid JSON, which the hub would refuse every time.
func TestHeartbeat_hostnameWithControlCharacters(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("the script reads the hostname from hostname(1) on Linux only")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	bin := t.TempDir()
	fake := "#!/bin/sh\nprintf 'web\\001-\\033[31m1\\177\\037\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "hostname"), []byte(fake), 0o700); err != nil { //nolint:gosec // G306: it has to run.
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), bash, "agent.sh", "--print") //nolint:gosec // G204: bash is the one on PATH.
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"), "BIELD_AGENT_CONF=/nonexistent",
		"SERVER_URL=https://bield.example.ts.net", "SECRET=not-a-real-secret")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("agent.sh --print: %v\n%s", err, out)
	}
	var beat struct {
		Hostname string `json:"hostname"`
	}
	if err := json.Unmarshal(out, &beat); err != nil {
		t.Fatalf("the heartbeat isn't valid JSON: %v\n%.200s", err, out)
	}
	if want := "web - [31m1  "; beat.Hostname != want {
		t.Errorf("hostname = %q, want %q, with the control characters turned into spaces", beat.Hostname, want)
	}
}
