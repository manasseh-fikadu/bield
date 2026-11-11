package api

import (
	"net"
	"net/http"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

func TestEditorSessions(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var (
			mu     sync.Mutex
			events []string
		)
		s := &editorSessions{log: func(k editorKey, opened bool) {
			mu.Lock()
			defer mu.Unlock()
			what := "closed"
			if opened {
				what = "opened"
			}
			events = append(events, k.login+" "+what+" "+k.accessID)
		}}
		logged := func() []string {
			synctest.Wait()
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(events)
		}
		alice := caller{who: &tailnet.Identity{LoginName: "alice"}, level: admins}
		bob := caller{who: &tailnet.Identity{LoginName: "bob"}, level: admins}
		open := func(id string, c caller) net.Conn {
			a, b := net.Pipe()
			t.Cleanup(func() { _ = b.Close() })
			return s.add(id, c, a)
		}

		// VS Code opens two WebSockets for a window. That's one open.
		w1, w2 := open("laptop", alice), open("laptop", alice)
		if got := logged(); !slices.Equal(got, []string{"alice opened laptop"}) {
			t.Fatalf("after alice opened it, events = %q", got)
		}
		// A reload closes them and opens new ones, within the minute.
		_ = w1.Close()
		_ = w2.Close()
		time.Sleep(20 * time.Second)
		w3 := open("laptop", alice)
		time.Sleep(2 * editorCloseGrace)
		if got := logged(); len(got) != 1 {
			t.Fatalf("after a reload, events = %q, want no close", got)
		}
		if by := s.openBy("laptop"); !slices.Equal(by, []string{"alice"}) {
			t.Errorf("open by %q", by)
		}

		// bob opens it too, and closing it twice counts once.
		b1 := open("laptop", bob)
		_ = b1.Close()
		_ = b1.Close()
		time.Sleep(editorCloseGrace + time.Second)
		want := []string{"alice opened laptop", "bob opened laptop", "bob closed laptop"}
		if got := logged(); !slices.Equal(got, want) {
			t.Fatalf("events = %q, want %q", got, want)
		}

		// Closing the device's editor closes alice's WebSocket, and logs the
		// close a minute later.
		s.closeDevice("laptop")
		if _, err := w3.Write([]byte("x")); err == nil {
			t.Error("alice's WebSocket is open after the device's closed")
		}
		if by := s.openBy("laptop"); len(by) != 0 {
			t.Errorf("open by %q after the close", by)
		}
		time.Sleep(editorCloseGrace + time.Second)
		want = append(want, "alice closed laptop")
		if got := logged(); !slices.Equal(got, want) {
			t.Fatalf("events = %q, want %q", got, want)
		}

		// When Bield stops, an open editor and one in its minute of grace are
		// both closed at once.
		open("laptop", alice)
		v := open("vps", bob)
		_ = v.Close()
		s.stop()
		got := logged()
		if len(got) != len(want)+4 || !slices.Contains(got[len(want)+2:], "alice closed laptop") ||
			!slices.Contains(got[len(want)+2:], "bob closed vps") {
			t.Errorf("after the stop, events = %q", got)
		}
		time.Sleep(2 * editorCloseGrace)
		if after := logged(); len(after) != len(got) {
			t.Errorf("a close was logged twice: %q", after)
		}
	})
}

func TestEditorCrossSite(t *testing.T) {
	t.Parallel()
	const origin = "https://bield.example.ts.net:4710"
	tests := []struct {
		name   string
		method string
		header http.Header
		ok     bool
	}{
		{"a page it opens itself", http.MethodGet, http.Header{"Sec-Fetch-Site": {"same-origin"}}, true},
		{"typed into the address bar", http.MethodGet, http.Header{"Sec-Fetch-Site": {"none"}}, true},
		{"curl", http.MethodPost, http.Header{}, true},
		{"its own WebSocket", http.MethodGet, http.Header{"Upgrade": {"websocket"}, "Origin": {origin}}, true},
		{"a WebSocket from Bield", http.MethodGet, http.Header{"Upgrade": {"websocket"}, "Origin": {"https://bield.example.ts.net"}}, false},
		{"a WebSocket without an origin", http.MethodGet, http.Header{"Upgrade": {"websocket"}}, false},
		{"a link from Bield", http.MethodGet, http.Header{
			"Sec-Fetch-Site": {"same-site"}, "Sec-Fetch-Mode": {"navigate"}, "Sec-Fetch-Dest": {"document"},
		}, true},
		{"a frame on Bield", http.MethodGet, http.Header{
			"Sec-Fetch-Site": {"same-site"}, "Sec-Fetch-Mode": {"navigate"}, "Sec-Fetch-Dest": {"iframe"},
		}, false},
		{"an image elsewhere", http.MethodGet, http.Header{
			"Sec-Fetch-Site": {"cross-site"}, "Sec-Fetch-Mode": {"no-cors"}, "Sec-Fetch-Dest": {"image"},
		}, false},
		{"a POST of an old browser from its own page", http.MethodPost, http.Header{"Origin": {origin}}, true},
		{"a POST of an old browser from elsewhere", http.MethodPost, http.Header{"Origin": {"https://bield.example.ts.net"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := http.NewRequestWithContext(t.Context(), tt.method, origin+"/", nil)
			if err != nil {
				t.Fatal(err)
			}
			r.Header = tt.header
			if why := editorCrossSite(r, origin); (why == "") != tt.ok {
				t.Errorf("editorCrossSite = %q, want allowed %v", why, tt.ok)
			}
		})
	}
}
