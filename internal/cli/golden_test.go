package cli_test

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/api/apitest"
)

// update rewrites the golden files with what the commands print now:
// go test ./internal/cli -run TestGolden -update. Read the diff before you
// commit it, since scripts and agents read this output.
var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// scrubs are what changes from run to run, and what each becomes in a golden
// file.
var scrubs = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`), "<time>"},
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}`), "<time>"},
	{regexp.MustCompile(`\b[0-9a-f]{16}\b`), "<check-id>"},
	{regexp.MustCompile(`"hint": "[A-Z2-7]{4}"`), `"hint": "<hint>"`},
	{regexp.MustCompile(`works until \d{1,2} [A-Z][a-z]+ \d{4}`), "works until <date>"},
}

func scrub(s string) string {
	for _, sc := range scrubs {
		s = sc.re.ReplaceAllString(s, sc.with)
	}
	return s
}

// TestGolden checks what the commands print, as a table and as JSON, against
// the files in testdata/golden, so a change to the output that scripts and
// agents read shows up in review. The tailnet is apitest's, with a check, an
// open alert and an API token, and alice, an admin, asks.
func TestGolden(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	run(t, s, apitest.LaptopIP, "checks", "add", "--name", "web", "--kind", "tcp", "--target", "100.64.0.4:443").want(t, 0, "", "")
	openAlert(t, s, "disk", "Disk 91% full on web-1")
	run(t, s, apitest.LaptopIP, "tokens", "create", "ci", "--days", "7").want(t, 0, "bield_", "")

	cases := [][]string{
		{"whoami"},
		{"status"},
		{"devices"},
		{"devices", "show", "laptop"},
		{"checks"},
		{"alerts"},
		{"access"},
		{"tokens"},
		{"events"},
	}
	for _, args := range cases {
		for _, asJSON := range []bool{false, true} {
			cmd, name := args, strings.Join(args, "-")+".txt"
			if asJSON {
				cmd, name = append(append([]string{}, args...), "--json"), strings.Join(args, "-")+".json"
			}
			r := run(t, s, apitest.LaptopIP, cmd...)
			if r.code != 0 {
				t.Errorf("bield %s = exit %d\n%s", strings.Join(cmd, " "), r.code, r.stderr)
				continue
			}
			golden(t, name, scrub(r.stdout))
		}
	}
	golden(t, "help.txt", run(t, s, apitest.LaptopIP, "help").stdout)
}

// golden compares got with the golden file called name, or writes it with
// -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // G304: a file in testdata.
	if err != nil {
		t.Fatalf("%v; run go test ./internal/cli -run TestGolden -update to write it", err)
	}
	if got != string(want) {
		t.Errorf("bield's output for %s changed; if that's meant, run go test ./internal/cli -run TestGolden -update "+
			"and commit the files\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}
