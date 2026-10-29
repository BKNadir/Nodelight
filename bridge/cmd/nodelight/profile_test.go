package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestLegacyConfigIsTheDefaultProfile(t *testing.T) {
	var cfg fileConfig
	legacy := `{"hostname":"nodelight","exitNode":"ha","hosts":[{"name":"bureau","address":"192.168.1.20"}]}`
	if err := json.Unmarshal([]byte(legacy), &cfg); err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.profile("")
	if !ok || p.Name != defaultProfileName || p.ExitNode != "ha" || len(p.Hosts) != 1 {
		t.Fatalf("got %+v, %v", p, ok)
	}
	if cfg.activeName() != defaultProfileName {
		t.Fatalf("active: %q", cfg.activeName())
	}
	if got := cfg.profileNames(); len(got) != 1 || got[0] != "default" {
		t.Fatalf("names: %v", got)
	}
}

func TestSetAndDeleteProfile(t *testing.T) {
	var cfg fileConfig
	cfg.setProfile(profile{Name: "Travail", Hosts: []hostSpec{{Name: "a", Address: "x"}}})
	cfg.setProfile(profile{Name: "travail", ExitNode: "nas"}) // same profile, other case: replaced
	if len(cfg.Profiles) != 1 || cfg.Profiles[0].Name != "Travail" || cfg.Profiles[0].ExitNode != "nas" {
		t.Fatalf("profiles: %+v", cfg.Profiles)
	}
	cfg.setProfile(profile{Name: "default", Hostname: "h"})
	if cfg.Hostname != "h" || len(cfg.Profiles) != 1 {
		t.Fatalf("default must be stored at the top level: %+v", cfg)
	}
	cfg.Active = "TRAVAIL"
	if cfg.activeName() != "Travail" {
		t.Fatalf("active: %q", cfg.activeName())
	}
	if !cfg.deleteProfile("travail") || cfg.deleteProfile("travail") {
		t.Fatal("delete should succeed once")
	}
	if cfg.Active != "" || cfg.activeName() != defaultProfileName {
		t.Fatalf("active after delete: %q", cfg.Active)
	}
}

func TestProfileStateDir(t *testing.T) {
	base := filepath.Join("x", "data")
	if profileStateDir(base, "") != base || profileStateDir(base, "Default") != base {
		t.Fatal("default profile must keep using the base directory")
	}
	if got := profileStateDir(base, "travail"); got != filepath.Join(base, "profiles", "travail") {
		t.Fatalf("got %q", got)
	}
}

func TestProfilesSurviveSaveAndLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts.json")
	cfg := fileConfig{Active: "travail", Hosts: []hostSpec{{Name: "d", Address: "1"}}}
	cfg.setProfile(profile{Name: "travail", ControlURL: "https://hs.example.org", Hosts: []hostSpec{{Name: "w", Address: "2"}}})
	if err := saveConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig(p)
	if err != nil || got.activeName() != "travail" {
		t.Fatalf("got %+v, %v", got, err)
	}
	w, _ := got.profile("travail")
	if w.ControlURL != "https://hs.example.org" || len(w.Hosts) != 1 || w.Hosts[0].Name != "w" {
		t.Fatalf("profile: %+v", w)
	}
}
