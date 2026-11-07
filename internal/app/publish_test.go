package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackgroundSetupStatusReportsCloudReceipt(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ready := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/health" {
			w.Write([]byte(`{"raw_ports_v1":true}`))
			return
		}
		if ready {
			w.Write([]byte(`{"cloud_ready":true,"cloud_message":"Regeln eingerichtet"}`))
		} else {
			w.Write([]byte(`{"cloud_ready":false,"cloud_message":"Oracle einmalig verbinden"}`))
		}
	}))
	defer server.Close()
	s := &Service{db: db, cfg: Config{Role: "client", ServerURL: server.URL}, firewallApply: func(context.Context) error { return nil }}
	if err = s.syncRoutes(); err != nil {
		t.Fatal(err)
	}
	if s.setupSnapshot().State != "needs_connection" {
		t.Fatal("missing IAM must not be reported as success")
	}
	ready = true
	if err = s.syncRoutes(); err != nil {
		t.Fatal(err)
	}
	if s.setupSnapshot().State != "configured" {
		t.Fatal("confirmed rules not shown")
	}
}

func TestServerStoresLocalRulesWithoutCloudAccess(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	caddy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer caddy.Close()
	dir := t.TempDir()
	// Explicitly disabled target avoids any metadata or cloud call in this test.
	if err = writeAtomic(filepath.Join(dir, "oci-target.json"), []byte(`{"enabled":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db, cfg: Config{Role: "server", Dir: dir, CaddyURL: caddy.URL}, firewallApply: func(context.Context) error { return nil }}
	out := httptest.NewRecorder()
	s.configAPI(out, httptest.NewRequest("POST", "/internal/config", strings.NewReader(`{"web":[],"ports":[{"id":1,"protocol":"tcp","target_ip":"192.168.1.29","target_port":7777,"public_port":7777,"enabled":true}]}`)))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"cloud_ready":false`) {
		t.Fatal(out.Body.String())
	}
	ports, _ := portForwards(db)
	if len(ports) != 1 || !ports[0].Enabled {
		t.Fatal("missing cloud credentials discarded local forwarding")
	}
}

func TestDevicePublicationPreservesMappings(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db, cfg: Config{Role: "client"}, routeChanges: make(chan struct{}, 1)}
	web := []Route{{ID: 1, Domain: "test.example.net", Backend: "http://192.168.1.95:8096", PublicPort: 8096, Enabled: true}}
	initial := []PortForward{{ID: 1, Name: "existing", Protocol: "tcp", TargetIP: "192.168.1.29", TargetPort: 7777, PublicPort: 7777, Enabled: true}, {ID: 2, Name: "draft", Protocol: "udp", TargetIP: "192.168.1.29", TargetPort: 7777, PublicPort: 7777}}
	if err = replaceAllRoutes(db, web, initial); err != nil {
		t.Fatal(err)
	}
	input := `[{"target_ip":"192.168.1.40","target_port":7777,"protocol":"tcp"},{"target_ip":"192.168.1.29","target_port":7777,"protocol":"udp"},{"target_ip":"192.168.1.95","target_port":8096,"protocol":"tcp"}]`
	for i := 0; i < 2; i++ {
		out := httptest.NewRecorder()
		s.publishPortsAPI(out, httptest.NewRequest("POST", "/api/ports/publish", strings.NewReader(input)))
		if out.Code != 202 {
			t.Fatal(out.Body.String())
		}
	}
	ports, _ := portForwards(db)
	if len(ports) != 3 {
		t.Fatalf("duplicates or overwritten routes: %+v", ports)
	}
	for _, p := range ports {
		if !p.Enabled {
			t.Fatal("draft not activated")
		}
		if p.TargetIP == "192.168.1.40" && p.PublicPort != 7778 {
			t.Fatal("collision not allocated safely")
		}
		if p.ID == 1 && p.PublicPort != 7777 {
			t.Fatal("existing mapping replaced")
		}
	}
	saved, _ := routes(db)
	if len(saved) != 1 || !saved[0].Enabled {
		t.Fatal("existing HTTPS site lost")
	}
	if s.setupSnapshot().State != "working" {
		t.Fatal("background status missing")
	}
	bad := httptest.NewRecorder()
	s.publishPortsAPI(bad, httptest.NewRequest("POST", "/api/ports/publish", strings.NewReader(`[{"target_ip":"8.8.8.8","target_port":7777,"protocol":"tcp"}]`)))
	if bad.Code != 400 {
		t.Fatal("public scan target accepted")
	}
	after, _ := portForwards(db)
	if len(after) != 3 {
		t.Fatal("invalid request partially committed")
	}
}
