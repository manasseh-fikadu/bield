package main

import (
	"strings"
	"testing"
)

func TestPin(t *testing.T) {
	t.Parallel()
	asset := func(name, digest string) string {
		return `{"name":"` + name + `","browser_download_url":"https://example.com/` + name + `","size":100,"digest":"` + digest + `"}`
	}
	sum := "sha256:" + strings.Repeat("a", 64)
	var assets []string
	for _, s := range systems {
		assets = append(assets, asset("code-server-1.2.3-"+s.os+"-"+s.arch+".tar.gz", sum))
	}
	assets = append(assets, asset("code-server_1.2.3_amd64.deb", sum))
	good := `{"tag_name":"v1.2.3","assets":[` + strings.Join(assets, ",") + `]}`
	rel, err := pin([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "1.2.3" || len(rel.Assets) != 4 || rel.Assets[0].SHA256 != strings.Repeat("a", 64) ||
		rel.Assets[0].Dir != "code-server-1.2.3-linux-amd64" {
		t.Errorf("pin = %+v", rel)
	}
	for name, raw := range map[string]string{
		"a prerelease":    `{"tag_name":"v1.2.3","prerelease":true,"assets":[` + strings.Join(assets, ",") + `]}`,
		"a missing build": `{"tag_name":"v1.2.3","assets":[` + strings.Join(assets[1:], ",") + `]}`,
		"no hash":         strings.ReplaceAll(good, sum, ""),
	} {
		if _, err := pin([]byte(raw)); err == nil {
			t.Errorf("pin(%s) worked", name)
		}
	}
}
