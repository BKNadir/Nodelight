package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseHostFlag(t *testing.T) {
	cases := []struct {
		in   string
		want hostSpec
		bad  bool
	}{
		{"bureau=desktop-pc", hostSpec{Name: "bureau", Address: "desktop-pc"}, false},
		{"bureau=desktop-pc:48989", hostSpec{Name: "bureau", Address: "desktop-pc", BasePort: 48989}, false},
		{"a=100.101.102.103", hostSpec{Name: "a", Address: "100.101.102.103"}, false},
		{"a=100.101.102.103:47989", hostSpec{Name: "a", Address: "100.101.102.103", BasePort: 47989}, false},
		{"a=fd7a:115c:a1e0::1", hostSpec{Name: "a", Address: "fd7a:115c:a1e0::1"}, false},
		{"a=[fd7a:115c:a1e0::1]:48989", hostSpec{Name: "a", Address: "fd7a:115c:a1e0::1", BasePort: 48989}, false},
		{"noequals", hostSpec{}, true},
		{"=addr", hostSpec{}, true},
		{"name=", hostSpec{}, true},
		{"a=host:notaport", hostSpec{}, true},
		{"a=host:0", hostSpec{}, true},
		{"a=host:70000", hostSpec{}, true},
	}
	for _, c := range cases {
		got, err := parseHostFlag(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q: expected an error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q: got %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
}

func TestMergeHostsKeepsOrderAndOverrides(t *testing.T) {
	file := []hostSpec{{Name: "a", Address: "1"}, {Name: "b", Address: "2"}}
	flags := []hostSpec{{Name: "B", Address: "22"}, {Name: "c", Address: "3"}}
	got := mergeHosts(file, flags)
	want := []hostSpec{{Name: "a", Address: "1"}, {Name: "B", Address: "22"}, {Name: "c", Address: "3"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	if cfg, err := loadConfig(filepath.Join(dir, "missing.json")); err != nil || len(cfg.Hosts) != 0 {
		t.Fatalf("missing file should yield an empty config, got %+v, %v", cfg, err)
	}
	p := filepath.Join(dir, "hosts.json")
	if err := os.WriteFile(p, []byte(`{"hostname":"pc-portable","hosts":[{"name":"bureau","address":"desktop-pc","basePort":48989}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(p)
	if err != nil || cfg.Hostname != "pc-portable" || len(cfg.Hosts) != 1 || cfg.Hosts[0].BasePort != 48989 {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	if err := os.WriteFile(p, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(p); err == nil {
		t.Fatal("invalid JSON must be reported")
	}
}

func TestPromptForHostAndSave(t *testing.T) {
	var out strings.Builder
	h, err := promptForHost(strings.NewReader("  desktop-pc:48989 \n"), &out)
	if err != nil || h != (hostSpec{Name: "pc", Address: "desktop-pc", BasePort: 48989}) {
		t.Fatalf("got %+v, %v", h, err)
	}
	if _, err := promptForHost(strings.NewReader("\n"), &out); err == nil {
		t.Fatal("an empty answer must be rejected")
	}
	if _, err := promptForHost(strings.NewReader(""), &out); err == nil {
		t.Fatal("end of input must be rejected")
	}

	p := filepath.Join(t.TempDir(), "hosts.json")
	if err := saveConfig(p, fileConfig{Hosts: []hostSpec{h}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(p)
	if err != nil || len(cfg.Hosts) != 1 || cfg.Hosts[0] != h {
		t.Fatalf("round trip failed: %+v, %v", cfg, err)
	}
}

func TestPromptsShareOneReader(t *testing.T) {
	// Both prompts read from the same stream, as they do from stdin.
	br := bufio.NewReader(strings.NewReader("192.168.1.20\nhomeassistant\n"))
	var out strings.Builder
	h, err := promptForHost(br, &out)
	if err != nil || h.Address != "192.168.1.20" {
		t.Fatalf("host: %+v, %v", h, err)
	}
	ex, err := promptForExitNode(br, &out)
	if err != nil || ex != "homeassistant" {
		t.Fatalf("exit node: %q, %v", ex, err)
	}
	// Empty answer and end of input both mean "no exit node".
	if ex, err := promptForExitNode(strings.NewReader("\n"), &out); err != nil || ex != "" {
		t.Fatalf("empty: %q, %v", ex, err)
	}
	if ex, err := promptForExitNode(strings.NewReader(""), &out); err != nil || ex != "" {
		t.Fatalf("eof: %q, %v", ex, err)
	}
}

func TestExitNodeRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts.json")
	if err := saveConfig(p, fileConfig{ExitNode: "homeassistant", Hosts: []hostSpec{{Name: "pc", Address: "192.168.1.20"}}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(p)
	if err != nil || cfg.ExitNode != "homeassistant" {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}
