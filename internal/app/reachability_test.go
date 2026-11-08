package app

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDialLANRejectsClosedEndpoint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, number, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(number)
	if err := dialLAN(context.Background(), "127.0.0.1", port); err != nil {
		t.Fatal(err)
	}
	listener.Close()
	if err := dialLAN(context.Background(), "127.0.0.1", port); err == nil {
		t.Fatal("closed listener accepted")
	}
}

func TestPublicationRechecksStaleDiscoveryAtomically(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db, cfg: Config{Role: "client"}, routeChanges: make(chan struct{}, 1)}
	s.checkLAN = func(_ context.Context, _ string, port int) error {
		if port == 18081 {
			return errors.New("connection refused")
		}
		return nil
	}
	_, err = db.Exec(`INSERT INTO scan_services(target_ip,target_port,name,backend) VALUES('192.168.50.20',18081,'Stale web','http://192.168.50.20:18081')`)
	if err != nil {
		t.Fatal(err)
	}
	out := httptest.NewRecorder()
	s.publishPortsAPI(out, httptest.NewRequest("POST", "/api/ports/publish", strings.NewReader(`[{"target_ip":"192.168.50.20","target_port":18080,"protocol":"tcp","auto":true},{"target_ip":"192.168.50.20","target_port":18081,"protocol":"tcp","auto":true,"domain":"public.example.net"}]`)))
	if out.Code != 409 || !strings.Contains(out.Body.String(), "18081") {
		t.Fatal(out.Code, out.Body.String())
	}
	ports, _ := portForwards(db)
	web, _ := routes(db)
	if len(ports) != 0 || len(web) != 0 || len(s.routeChanges) != 0 {
		t.Fatal("failed selection partially published or notified firewall")
	}
	s.checkLAN = func(context.Context, string, int) error { return nil }
	out = httptest.NewRecorder()
	s.publishPortsAPI(out, httptest.NewRequest("POST", "/api/ports/publish", strings.NewReader(`[{"target_ip":"192.168.50.20","target_port":18080,"protocol":"tcp","auto":true}]`)))
	if out.Code != 202 {
		t.Fatal(out.Code, out.Body.String())
	}
	ports, _ = portForwards(db)
	if len(ports) != 1 || !ports[0].Enabled {
		t.Fatal("reachable TCP service rejected")
	}
}

func TestNewScanReplacesOnlySelectedDeviceDiscoveries(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db}
	for _, ip := range []string{"192.168.50.20", "192.168.50.30"} {
		for _, port := range []int{18080, 18081} {
			if _, err = db.Exec(`INSERT INTO scan_results(target_ip,target_port,protocol,found_at) VALUES(?,?,'tcp','old')`, ip, port); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`INSERT INTO scan_services(target_ip,target_port,name,backend) VALUES(?,?,'old','')`, ip, port); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = replaceAllRoutes(db, nil, []PortForward{
		{ID: 1, Protocol: "tcp", TargetIP: "192.168.50.20", TargetPort: 18080, PublicPort: 18080},
		{ID: 2, Protocol: "tcp", TargetIP: "192.168.50.20", TargetPort: 18081, PublicPort: 18081, Enabled: true},
		{ID: 3, Protocol: "tcp", TargetIP: "192.168.50.20", TargetPort: 18082, PublicPort: 18082},
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.clearScannedTargets([]string{"192.168.50.20"}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"scan_results", "scan_services"} {
		var count int
		db.QueryRow("SELECT count(*) FROM " + table + " WHERE target_ip='192.168.50.20'").Scan(&count)
		if count != 0 {
			t.Fatal("stale selected-device discoveries retained", table)
		}
		db.QueryRow("SELECT count(*) FROM " + table + " WHERE target_ip='192.168.50.30'").Scan(&count)
		if count != 2 {
			t.Fatal("unrelated device discoveries removed", table)
		}
	}
	ports, _ := portForwards(db)
	if len(ports) != 2 || ports[0].ID != 2 || !ports[0].Enabled || ports[1].ID != 3 {
		t.Fatal("active service or manual draft removed", ports)
	}
}
