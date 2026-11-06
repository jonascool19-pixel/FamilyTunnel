package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestActualCaddyAdapterPorts(t *testing.T) {
	binary := os.Getenv("CADDY_BIN")
	if binary == "" {
		t.Skip("set CADDY_BIN for real Caddy adapter verification")
	}
	config := caddyfile([]Route{{ID: 1, Domain: "test.example.net", PublicPort: 443, Enabled: true}, {ID: 2, Domain: "test.example.net", PublicPort: 8096, Enabled: true}, {ID: 3, Domain: "test.example.net", PublicPort: 8123, Enabled: true}})
	path := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(binary, "adapt", "--config", path, "--adapter", "caddyfile").Output()
	if err != nil {
		t.Fatalf("Caddy rejected generated config: %v", err)
	}
	var adapted struct {
		Apps struct {
			HTTP struct {
				Servers map[string]struct {
					Listen []string `json:"listen"`
				} `json:"servers"`
			} `json:"http"`
		} `json:"apps"`
	}
	if err = json.Unmarshal(output, &adapted); err != nil {
		t.Fatal(err)
	}
	listeners := map[string]bool{}
	for _, server := range adapted.Apps.HTTP.Servers {
		for _, address := range server.Listen {
			listeners[address] = true
		}
	}
	for _, port := range []string{":443", ":8096", ":8123"} {
		if !listeners[port] {
			t.Fatalf("missing listener %s: %s", port, output)
		}
	}
}

func TestPortsMigrationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE routes(id INTEGER PRIMARY KEY,domain TEXT UNIQUE NOT NULL,backend TEXT NOT NULL,enabled INTEGER NOT NULL);INSERT INTO routes VALUES(9,'test.example.net','http://192.168.1.5:8096',1);`)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := routes(db)
	if err != nil || len(rs) != 1 || rs[0].ID != 9 || rs[0].port() != 443 {
		t.Fatalf("legacy route lost: %v %v", rs, err)
	}
	rs = append(rs, Route{ID: 10, Domain: rs[0].Domain, Backend: "http://192.168.1.6:8123", PublicPort: 8123, Enabled: true})
	if err = replaceRoutes(db, rs); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO routes(domain,backend,enabled,public_port) VALUES(?,?,1,8123)", rs[0].Domain, rs[0].Backend); err == nil {
		t.Fatal("duplicate port accepted")
	}
	db.Close()
	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rs, err = routes(db)
	if err != nil || len(rs) != 2 || rs[1].port() != 8123 {
		t.Fatalf("ports lost on restart: %v %v", rs, err)
	}
}

func TestReservedPublicPorts(t *testing.T) {
	for _, port := range []int{-1, 22, 80, 2019, 8443, 9080, 9081, 65536} {
		r := Route{Domain: "test.example.net", Backend: "http://192.168.1.5:8096", PublicPort: port}
		if r.validate() == nil {
			t.Fatalf("accepted reserved port %d", port)
		}
	}
}

func TestPortRouteCRUDAndOldServerGuard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db, cfg: Config{Role: "client", ServerURL: server.URL, Token: "test"}}
	for _, body := range []string{`{"domain":"test.example.net","backend":"http://192.168.1.5:8096","enabled":true}`, `{"domain":"test.example.net","backend":"http://192.168.1.6:8123","public_port":8123,"enabled":true}`} {
		req := httptest.NewRequest("POST", "/api/routes", strings.NewReader(body))
		out := httptest.NewRecorder()
		s.routeAPI(out, req)
		if out.Code != 200 && out.Code != 202 {
			t.Fatalf("create failed: %d %s", out.Code, out.Body.String())
		}
	}
	if err = s.syncRoutes(); err == nil || !strings.Contains(err.Error(), "aktualisieren") {
		t.Fatalf("old server not detected: %v", err)
	}
	rs, err := routes(db)
	if err != nil || len(rs) != 2 {
		t.Fatal("multiport routes not saved")
	}
	req := httptest.NewRequest("PUT", "/api/routes/2", strings.NewReader(`{"domain":"test.example.net","backend":"http://192.168.1.6:8123","public_port":9000,"enabled":false}`))
	out := httptest.NewRecorder()
	s.routeAPI(out, req)
	rs, err = routes(db)
	if err != nil || rs[1].PublicPort != 9000 || rs[1].Enabled {
		t.Fatalf("edit/disable failed: %v %v", rs, err)
	}
	req = httptest.NewRequest("DELETE", "/api/routes/2", nil)
	out = httptest.NewRecorder()
	s.routeAPI(out, req)
	rs, err = routes(db)
	if err != nil || len(rs) != 1 || rs[0].port() != 443 {
		t.Fatalf("delete damaged root route: %v %v", rs, err)
	}
}

func TestCaddyPortsAndBackendIsolation(t *testing.T) {
	backends := []*httptest.Server{}
	rs := []Route{}
	for i, port := range []int{443, 8096, 8123} {
		name := []string{"root", "media", "home"}[i]
		b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(name)) }))
		defer b.Close()
		backends = append(backends, b)
		rs = append(rs, Route{ID: int64(i + 1), Domain: "test.example.net", Backend: b.URL, PublicPort: port, Enabled: true})
	}
	config := caddyfile(rs)
	for _, r := range rs {
		if !strings.Contains(config, "https://"+r.address()+" {") {
			t.Fatalf("missing site %s", r.address())
		}
	}
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Local mock backends bypass LAN-only validation; production API still validates.
	for _, r := range rs {
		if _, err = db.Exec("INSERT INTO routes(id,domain,backend,enabled,public_port) VALUES(?,?,?,1,?)", r.ID, r.Domain, r.Backend, r.port()); err != nil {
			t.Fatal(err)
		}
	}
	s := &Service{db: db, cfg: Config{Token: "secret"}, traffic: traffic{data: map[string]counters{}}}
	for i, r := range rs {
		req := httptest.NewRequest("GET", "https://"+r.address()+"/", nil)
		req.RemoteAddr = "10.77.0.1:5555"
		nonce := strings.Repeat("b", 43)
		req.Header.Set("X-FamilyTunnel-Probe", nonce)
		out := httptest.NewRecorder()
		s.proxy(out, req)
		if out.Body.String() != []string{"root", "media", "home"}[i] {
			t.Fatalf("port %d routed to %s", r.port(), out.Body.String())
		}
		if out.Header().Get("X-FamilyTunnel-Proof") != publicProof("secret", nonce, r.address()) {
			t.Fatal("wrong port proof")
		}
	}
	req := httptest.NewRequest("GET", "https://test.example.net:9999/", nil)
	req.RemoteAddr = "10.77.0.1:5555"
	out := httptest.NewRecorder()
	s.proxy(out, req)
	if out.Code != 404 {
		t.Fatal("unpublished port exposed a backend")
	}
	rs[1].Enabled = false
	if strings.Contains(caddyfile(rs), "https://test.example.net:8096 {") {
		t.Fatal("disabled port still in Caddy")
	}
}

func TestPublicTestRequiresExactPort(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = replaceRoutes(db, []Route{{ID: 1, Domain: "test.example.net", Backend: "http://192.168.1.5:8096", PublicPort: 8096, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db, cfg: Config{Role: "client"}}
	req := httptest.NewRequest("POST", "/api/public-test", strings.NewReader(`{"domain":"test.example.net","public_port":8123}`))
	out := httptest.NewRecorder()
	s.publicTestAPI(out, req)
	if out.Code != 409 {
		t.Fatalf("wrong port accepted: %d", out.Code)
	}
}
