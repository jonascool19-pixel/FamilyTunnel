package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestForwardValidation(t *testing.T) {
	good := PortForward{Protocol: "udp", TargetIP: "192.168.1.29", TargetPort: 7777, PublicPort: 17777}
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{0, 22, 443, 2019, 8443, 9080, 9081, 51820, 65536} {
		p := good
		p.PublicPort = port
		if p.validate() == nil {
			t.Fatalf("invalid UDP public port accepted: %d", port)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.77.0.1", "169.254.169.254", "8.8.8.8", "::1"} {
		p := good
		p.TargetIP = ip
		if p.validate() == nil {
			t.Fatalf("invalid LAN target accepted: %s", ip)
		}
	}
	good.PublicPort = 8096
	good.Protocol = "tcp"
	if validateForwardSet([]Route{{Domain: "test.example.net", PublicPort: 8096, Enabled: true}}, []PortForward{good}) != nil {
		t.Fatal("inactive proposal must not reserve a web port")
	}
	good.Enabled = true
	if validateForwardSet([]Route{{Domain: "test.example.net", PublicPort: 8096, Enabled: true}}, []PortForward{good}) == nil {
		t.Fatal("TCP/HTTPS collision accepted")
	}
	good.Protocol = "udp"
	if validateForwardSet([]Route{{Domain: "test.example.net", PublicPort: 8096, Enabled: true}}, []PortForward{good}) != nil {
		t.Fatal("UDP must coexist with HTTPS on same number")
	}
}

func TestPortBatchDraftAndActivation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db, cfg: Config{Role: "client"}, routeChanges: make(chan struct{}, 1)}
	input := `[{"name":"Game","protocol":"tcp","target_ip":"192.168.1.29","target_port":7777,"public_port":7777,"enabled":true},{"name":"Game","protocol":"udp","target_ip":"192.168.1.29","target_port":7777,"public_port":7777,"enabled":true}]`
	out := httptest.NewRecorder()
	s.portAPI(out, httptest.NewRequest("POST", "/api/ports/import", strings.NewReader(input)))
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	ps, _ := portForwards(db)
	if len(ps) != 2 || ps[0].Enabled || ps[1].Enabled {
		t.Fatalf("import published services: %+v", ps)
	}
	out = httptest.NewRecorder()
	s.portAPI(out, httptest.NewRequest("POST", "/api/ports/activate", strings.NewReader(`{"ids":[1,2]}`)))
	ps, _ = portForwards(db)
	if out.Code != 202 || !ps[0].Enabled || !ps[1].Enabled {
		t.Fatal("bulk activation failed")
	}
	out = httptest.NewRecorder()
	s.portAPI(out, httptest.NewRequest("POST", "/api/ports/batch", strings.NewReader(input)))
	if out.Code != 409 {
		t.Fatal("duplicate batch must fail atomically")
	}
	out = httptest.NewRecorder()
	s.portAPI(out, httptest.NewRequest("DELETE", "/api/ports/1", nil))
	ps, _ = portForwards(db)
	if out.Code != 202 || len(ps) != 1 || ps[0].Protocol != "udp" {
		t.Fatal("delete changed other protocol")
	}
}

func TestServerPortFirewallRollback(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	caddy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	defer caddy.Close()
	s := &Service{db: db, cfg: Config{Role: "server", CaddyURL: caddy.URL}, firewallApply: func(context.Context) error { return errors.New("test: sysctl denied") }}
	out := httptest.NewRecorder()
	s.configAPI(out, httptest.NewRequest("POST", "/internal/config", strings.NewReader(`{"web":[],"ports":[{"id":1,"protocol":"udp","target_ip":"192.168.1.29","target_port":7777,"public_port":7777,"enabled":true}]}`)))
	if out.Code != 503 || !strings.Contains(out.Body.String(), "zurückgerollt") {
		t.Fatal(out.Body.String())
	}
	ps, _ := portForwards(db)
	if len(ps) != 0 {
		t.Fatal("failed firewall change remained stored")
	}
}

func TestFullScanPlanAndCancellation(t *testing.T) {
	ips, ports, err := scanPlan("192.168.1.10", "192.168.1.29,192.168.1.95", "full")
	if err != nil || len(ips) != 2 || len(ports) != 65535 || ports[0] != 1 || ports[65534] != 65535 {
		t.Fatal("full scan plan incomplete")
	}
	if _, _, err = scanPlan("192.168.1.10", "8.8.8.8", "full"); err == nil {
		t.Fatal("public scan accepted")
	}
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	s := &Service{db: db}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := &scanJob{Running: true, Total: 1, cancel: cancel, finished: make(chan struct{})}
	s.scan = job
	s.runPortScan(ctx, job, []string{"127.0.0.1"}, []int{port})
	if job.Running || job.Done != 1 || job.Found != 1 {
		t.Fatalf("real TCP result missing: %+v", job)
	}
	ps, _ := portForwards(db)
	if len(ps) != 0 {
		t.Fatal("loopback scan must never become a public proposal")
	}
	var count int
	_ = db.QueryRow("SELECT count(*) FROM scan_results").Scan(&count)
	if count != 1 {
		t.Fatal("results were not persisted")
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	job = &scanJob{Running: true, cancel: cancel, finished: make(chan struct{})}
	s.scan = job
	s.runPortScan(ctx, job, []string{"192.168.1.99"}, []int{65535})
	if job.Running || job.Done != 0 {
		t.Fatal("cancel did not stop scan")
	}
}
