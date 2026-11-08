package app

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshInstallStartsWithoutServicesOrPersonalSettings(t *testing.T) {
	var previousPassword string
	for _, role := range []string{"server", "client"} {
		dir := t.TempDir()
		s, err := newService(Config{Role: role, Dir: dir, Listen: "192.168.50.10:8443"})
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"routes", "port_forwards", "scan_results", "scan_services", "stats"} {
			var count int
			if err = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
				t.Fatalf("fresh %s contains %s rows: %d (%v)", role, table, count, err)
			}
		}
		if s.discoveryDomain() != "" {
			t.Fatal("fresh install has a domain")
		}
		state, err := s.maintenanceState()
		if err != nil || state.Plan.Enabled || state.Job != nil {
			t.Fatal("fresh install has active maintenance", state, err)
		}
		local, err := s.maintenanceLocal()
		if err != nil || local.Plan.Enabled || local.Test != "" || local.Consumed != "" {
			t.Fatal("fresh install has local reboot consent")
		}
		for _, name := range []string{"oci-target.json", "site-verification.json"} {
			if _, err = os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatal("fresh install has account-specific file", name)
			}
		}
		password, err := os.ReadFile(filepath.Join(dir, "initial-password"))
		if err != nil || len(strings.TrimSpace(string(password))) < 12 {
			t.Fatal("initial password missing")
		}
		if previousPassword == string(password) {
			t.Fatal("initial password reused between installations")
		}
		previousPassword = string(password)
		if role == "client" {
			out := httptest.NewRecorder()
			s.scanAPI(out, httptest.NewRequest("GET", "/api/scan", nil))
			var scan struct {
				Results []any  `json:"results"`
				Domain  string `json:"domain"`
				Running bool   `json:"running"`
			}
			if err = json.Unmarshal(out.Body.Bytes(), &scan); err != nil || len(scan.Results) != 0 || scan.Domain != "" || scan.Running {
				t.Fatal("fresh discovery is not empty", out.Body.String())
			}
		}
		s.db.Close()
		// Opening the same installation again must not seed data or replace login.
		s, err = newService(Config{Role: role, Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		p, err := os.ReadFile(filepath.Join(dir, "initial-password"))
		if err != nil || string(p) != string(password) {
			t.Fatal("repeat startup changed credentials")
		}
		s.db.Close()
	}
}
