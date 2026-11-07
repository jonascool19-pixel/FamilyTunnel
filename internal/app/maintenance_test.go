package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testBootA = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
const testBootB = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
const testBootC = "cccccccc-cccc-cccc-cccc-cccccccccccc"
const testGrant = "test-grant-abcdefghijklmnop"

func maintenanceTestService(t *testing.T, role string) *Service {
	t.Helper()
	db, e := openDB(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return &Service{db: db, cfg: Config{Role: role, Token: testGrant}, routeChanges: make(chan struct{}, 1)}
}
func TestCoordinatedMaintenanceRequiresActualBootsAndStablePeer(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)
	serverBoot, clientBoot := testBootA, testBootA
	serverReboots, clientReboots := 0, 0
	serverReady := true
	server := maintenanceTestService(t, "server")
	client := maintenanceTestService(t, "client")
	server.maintenance = &maintenanceRuntime{Now: func() time.Time { return now }, Boot: func() string { return serverBoot }, Ready: func(context.Context) bool { return serverReady }, Uptime: func() time.Duration { return 5 * time.Minute }, Reboot: func(context.Context) error { serverReboots++; return nil }}
	client.maintenance = &maintenanceRuntime{Now: func() time.Time { return now }, Boot: func() string { return clientBoot }, Ready: func(context.Context) bool { return true }, Uptime: func() time.Duration { return 5 * time.Minute }, Reboot: func(context.Context) error { clientReboots++; return nil }}
	host := httptest.NewServer(server.internalHandlers())
	defer host.Close()
	client.cfg.ServerURL = host.URL
	request := httptest.NewRequest("POST", "/api/maintenance", strings.NewReader(`{"action":"test","confirmation":"RESTART_BOTH"}`))
	out := httptest.NewRecorder()
	client.maintenanceAPI(out, request)
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	if e := server.maintenanceServerTick(ctx); e != nil {
		t.Fatal(e)
	}
	if serverReboots != 0 {
		t.Fatal("reboot without fresh CT consent")
	}
	if e := client.maintenanceClientTick(ctx); e != nil {
		t.Fatal(e)
	}
	if e := server.maintenanceServerTick(ctx); e != nil {
		t.Fatal(e)
	}
	if serverReboots != 1 || clientReboots != 0 {
		t.Fatal("Oracle must go first")
	}
	for i := 0; i < 3; i++ {
		now = now.Add(30 * time.Second)
		_ = client.maintenanceClientTick(ctx)
		_ = server.maintenanceServerTick(ctx)
	}
	if clientReboots != 0 || serverReboots != 1 {
		t.Fatal("service restart or duplicate ticks triggered reboot")
	}
	serverBoot = testBootB
	for i := 0; i < 2; i++ {
		now = now.Add(30 * time.Second)
		_ = client.maintenanceClientTick(ctx)
		_ = server.maintenanceServerTick(ctx)
	}
	if clientReboots != 0 {
		t.Fatal("CT rebooted before one stable minute")
	}
	// A failed Oracle health check resets the complete stability period.
	serverReady = false
	now = now.Add(30 * time.Second)
	_ = client.maintenanceClientTick(ctx)
	_ = server.maintenanceServerTick(ctx)
	serverReady = true
	for i := 0; i < 3; i++ {
		now = now.Add(30 * time.Second)
		if e := client.maintenanceClientTick(ctx); e != nil {
			t.Fatal(e)
		}
		if e := server.maintenanceServerTick(ctx); e != nil {
			t.Fatal(e)
		}
	}
	v, _ := server.maintenanceState()
	if v.Job.State != "waiting_client" {
		t.Fatalf("state: %+v", v.Job)
	}
	if e := client.maintenanceClientTick(ctx); e != nil {
		t.Fatal(e)
	}
	if clientReboots != 1 {
		t.Fatal("released CT did not reboot")
	}
	for i := 0; i < 3; i++ {
		_ = client.maintenanceClientTick(ctx)
		_ = server.maintenanceServerTick(ctx)
	}
	if clientReboots != 1 || serverReboots != 1 {
		t.Fatal("duplicate reboot")
	}
	clientBoot = testBootC
	if e := client.maintenanceClientTick(ctx); e != nil {
		t.Fatal(e)
	}
	v, _ = server.maintenanceState()
	if v.Job.State != "completed" {
		t.Fatalf("completion not confirmed: %+v", v.Job)
	}
	local, e := client.maintenanceLocal()
	if e != nil || local.Consumed != v.Job.ID {
		t.Fatal("durable CT claim missing")
	}
}
func TestMaintenanceFailureExpiryAndDisabledDefaults(t *testing.T) {
	s := maintenanceTestService(t, "server")
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)
	calls := 0
	s.maintenance = &maintenanceRuntime{Now: func() time.Time { return now }, Boot: func() string { return testBootA }, Ready: func(context.Context) bool { return true }, Uptime: func() time.Duration { return time.Hour }, Reboot: func(context.Context) error { calls++; return errors.New("denied") }}
	if e := s.maintenanceServerTick(ctx); e != nil {
		t.Fatal(e)
	}
	if calls != 0 {
		t.Fatal("defaults rebooted")
	}
	_ = s.mutateMaintenance("maintenance_state", func(v *maintenanceState) error {
		v.Seen = now
		v.ClientReady = true
		v.ClientBoot = testBootB
		v.TestGrant = testGrant
		v.Job = &maintenanceJob{ID: testGrant, Grant: testGrant, Source: "test", State: "requested", Created: now, Expires: now.Add(time.Minute), ClientBefore: testBootB}
		return nil
	})
	if e := s.maintenanceServerTick(ctx); e == nil {
		t.Fatal("failed reboot not reported")
	}
	_ = s.maintenanceServerTick(ctx)
	if calls != 1 {
		t.Fatal("failed reboot retried")
	}
	_ = s.mutateMaintenance("maintenance_state", func(v *maintenanceState) error {
		v.Job.State = "server_rebooting"
		v.Job.Expires = now.Add(-time.Second)
		return nil
	})
	_ = s.maintenanceServerTick(ctx)
	v, _ := s.maintenanceState()
	if v.Job.State != "failed" || calls != 1 {
		t.Fatal("expired job was replayed")
	}
}
func TestMaintenanceCalendarAndConsent(t *testing.T) {
	p := maintenancePlan{Enabled: true, Time: "02:30", Zone: "Europe/Berlin", Mode: "daily", Generation: testGrant}
	if e := p.validate(); e != nil {
		t.Fatal(e)
	}
	if next := nextMaintenanceTime(p, time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)); next != time.Date(2026, 3, 30, 0, 30, 0, 0, time.UTC) {
		t.Fatal("nonexistent spring hour was shifted", next)
	}
	if next := nextMaintenanceTime(p, time.Date(2026, 3, 28, 3, 0, 0, 0, time.UTC)); next != time.Date(2026, 3, 30, 0, 30, 0, 0, time.UTC) {
		t.Fatal("tomorrow's nonexistent hour was not skipped", next)
	}
	state := maintenanceState{Plan: p}
	// Repeated 02:30 at the autumn clock change is only one daily slot.
	if !maintenanceDue(&state, time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) {
		t.Fatal("first slot missing")
	}
	if maintenanceDue(&state, time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) {
		t.Fatal("DST repeated a daily reboot")
	}
	state = maintenanceState{Plan: p}
	state.Plan.Mode = "interval24"
	state.Plan.Next = time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	if !maintenanceDue(&state, state.Plan.Next) {
		t.Fatal("24h slot missing")
	}
	if state.Plan.Next != time.Date(2026, 10, 26, 0, 30, 0, 0, time.UTC) {
		t.Fatal("24h interval changed with DST")
	}
	missed := state.Plan.Next.Add(4 * time.Hour)
	if maintenanceDue(&state, missed) {
		t.Fatal("missed reboot caught up")
	}
	s := maintenanceTestService(t, "client")
	out := httptest.NewRecorder()
	s.maintenanceAPI(out, httptest.NewRequest("POST", "/api/maintenance", strings.NewReader(`{"action":"test"}`)))
	if out.Code != http.StatusBadRequest {
		t.Fatal("test without explicit confirmation accepted")
	}
	for _, role := range []string{"client", "server"} {
		s := maintenanceTestService(t, role)
		s.auth = newAuth(s.db, t.TempDir())
		for _, path := range []string{"/api/maintenance", "/api/scan/reset", "/api/stats"} {
			out := httptest.NewRecorder()
			s.handlers().ServeHTTP(out, httptest.NewRequest("POST", path, nil))
			if out.Code != 401 {
				t.Fatalf("unauthenticated endpoint %s: %d", path, out.Code)
			}
		}
	}
}

func TestMaintenanceReleasesAfterExactlyOneStableMinute(t *testing.T) {
	s := maintenanceTestService(t, "server")
	start := time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)
	now := start.Add(59 * time.Second)
	s.maintenance = &maintenanceRuntime{Now: func() time.Time { return now }, Boot: func() string { return testBootB }, Ready: func(context.Context) bool { return true }, Uptime: func() time.Duration { return now.Sub(start) }, Reboot: func(context.Context) error { t.Fatal("unexpected reboot"); return nil }}
	err := s.mutateMaintenance("maintenance_state", func(v *maintenanceState) error {
		v.Seen = now
		v.ClientReady = true
		v.ClientBoot = testBootA
		v.TestGrant = testGrant
		v.Job = &maintenanceJob{ID: testGrant, Grant: testGrant, Source: "test", State: "server_rebooting", Created: start, Expires: start.Add(20 * time.Minute), ServerBefore: testBootA, ClientBefore: testBootA, StableSince: start, ReadyAt: now.Add(-30 * time.Second)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.maintenanceServerTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, _ := s.maintenanceState()
	if v.Job.State != "server_rebooting" {
		t.Fatal("released before 60 seconds")
	}
	now = start.Add(time.Minute)
	if err = s.maintenanceServerTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, _ = s.maintenanceState()
	if v.Job.State != "waiting_client" {
		t.Fatalf("not released after 60 seconds: %+v", v.Job)
	}
}
