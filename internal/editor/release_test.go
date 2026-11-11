package editor

import (
	"strings"
	"testing"
)

// The pinned release has a build, with a hash and a folder, for every system
// Bield installs on.
func TestPinned(t *testing.T) {
	t.Parallel()
	r := Pinned()
	for _, sys := range []struct{ system, machine string }{
		{"Linux", "x86_64"}, {"Linux", "aarch64"}, {"Darwin", "x86_64"}, {"Darwin", "arm64"},
	} {
		a, ok := r.Asset(sys.system, sys.machine)
		if !ok {
			t.Errorf("no build for %s on %s", sys.system, sys.machine)
			continue
		}
		if len(a.SHA256) != 64 || a.Size < 50<<20 || !strings.HasPrefix(a.URL, "https://github.com/coder/code-server/releases/download/v"+r.Version+"/") ||
			!strings.HasSuffix(a.URL, "/"+a.Dir+".tar.gz") {
			t.Errorf("the build for %s on %s = %+v", sys.system, sys.machine, a)
		}
	}
	for _, sys := range []struct{ system, machine string }{{"FreeBSD", "amd64"}, {"Linux", "armv7l"}, {"Linux", "i686"}} {
		if a, ok := r.Asset(sys.system, sys.machine); ok {
			t.Errorf("a build for %s on %s: %+v", sys.system, sys.machine, a)
		}
	}
}
