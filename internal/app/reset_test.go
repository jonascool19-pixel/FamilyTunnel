package app

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestStatsResetCannotReappearFromBuffer(t *testing.T) {
	s := maintenanceTestService(t, "client")
	s.traffic.data = map[string]counters{"media.example.net|2026-11-08T00:00:00Z": {Requests: 12, TX: 100}}
	s.flushStats()
	s.traffic.add("media.example.net|2026-11-08T00:00:00Z", counters{Requests: 5})
	out := httptest.NewRecorder()
	s.resetStatsAPI(out, httptest.NewRequest("DELETE", "/api/stats", nil))
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	s.flushStats()
	var n int
	s.db.QueryRow("SELECT count(*) FROM stats").Scan(&n)
	if n != 0 {
		t.Fatal("old counters reappeared")
	}
	s.traffic.add("media.example.net|2026-11-08T00:00:00Z", counters{Requests: 1})
	s.flushStats()
	s.db.QueryRow("SELECT sum(requests) FROM stats").Scan(&n)
	if n != 1 {
		t.Fatal("new traffic not counted")
	}
}
func TestScanResetWaitsForWorkerAndPreservesActiveServices(t *testing.T) {
	s := maintenanceTestService(t, "client")
	ctx, cancel := context.WithCancel(context.Background())
	s.scan = &scanJob{Running: true, cancel: cancel, finished: make(chan struct{})}
	_, e := s.db.Exec(`INSERT INTO port_forwards(name,protocol,target_ip,target_port,public_port,enabled) VALUES('active','tcp','192.168.1.2',8096,8096,1),('found','tcp','192.168.1.2',8920,8920,0),('manual','udp','192.168.1.3',7777,7777,0);INSERT INTO scan_results(target_ip,target_port,protocol,found_at) VALUES('192.168.1.2',8096,'tcp','now'),('192.168.1.2',8920,'tcp','now')`)
	if e != nil {
		t.Fatal(e)
	}
	go func() {
		<-ctx.Done()
		_ = setSetting(s.db, "last_port_scan", `{"running":false,"found":99}`)
		close(s.scan.finished)
	}()
	out := httptest.NewRecorder()
	s.resetScanAPI(out, httptest.NewRequest("POST", "/api/scan/reset", nil))
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM scan_results").Scan(&n)
	if n != 0 || setting(s.db, "last_port_scan") != "" || s.scan != nil {
		t.Fatal("old scan resurrected")
	}
	ps, _ := portForwards(s.db)
	if len(ps) != 2 || ps[0].Name != "active" || ps[1].Name != "manual" {
		t.Fatalf("wrong routes removed: %+v", ps)
	}
}
