package app

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestClearEventsKeepsConfigurationAndStillRecordsNewEvents(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db, cfg: Config{Role: "client"}, auth: newAuth(db, t.TempDir())}
	event(db, "test", "first")
	event(db, "test", "second")
	_ = setSetting(db, "discovery_domain", "public.example.net")
	web := []Route{{ID: 1, Name: "Media", Domain: "public.example.net", Backend: "http://192.168.1.95:8096", Enabled: true}}
	if err = replaceRoutes(db, web); err != nil {
		t.Fatal(err)
	}
	denied := httptest.NewRecorder()
	s.handlers().ServeHTTP(denied, httptest.NewRequest("DELETE", "/api/logs", nil))
	if denied.Code != 401 {
		t.Fatal("log deletion requires login")
	}
	out := httptest.NewRecorder()
	s.logsAPI(out, httptest.NewRequest("DELETE", "/api/logs", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"deleted":2`) {
		t.Fatal(out.Body.String())
	}
	rs, _ := routes(db)
	if len(rs) != 1 || setting(db, "discovery_domain") != "public.example.net" {
		t.Fatal("clearing events changed configuration")
	}
	out = httptest.NewRecorder()
	s.logsAPI(out, httptest.NewRequest("GET", "/api/logs", nil))
	if strings.TrimSpace(out.Body.String()) != "[]" {
		t.Fatal("events not cleared")
	}
	event(db, "test", "new event")
	out = httptest.NewRecorder()
	s.logsAPI(out, httptest.NewRequest("GET", "/api/logs", nil))
	if !strings.Contains(out.Body.String(), "new event") {
		t.Fatal("logging stopped after clear")
	}
}

func TestPortTestsDoNotClaimUDPOrProbeArbitraryPublicTargets(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db, cfg: Config{Role: "client", PublicIP: "127.0.0.1"}}
	ps := []PortForward{{ID: 1, Protocol: "udp", TargetIP: "192.168.1.29", TargetPort: 7777, PublicPort: 7777, Enabled: true}, {ID: 2, Protocol: "tcp", TargetIP: "192.168.1.29", TargetPort: 7777, PublicPort: 7777, Enabled: true}, {ID: 3, Protocol: "tcp", TargetIP: "192.168.1.40", TargetPort: 8096, PublicPort: 8096}}
	if err = replaceAllRoutes(db, []Route{}, ps); err != nil {
		t.Fatal(err)
	}
	for _, where := range []string{"local", "public"} {
		out := httptest.NewRecorder()
		s.portTestAPI(out, httptest.NewRequest("POST", "/api/port-test", strings.NewReader(`{"id":1,"location":"`+where+`"}`)))
		if out.Code != 200 || !strings.Contains(out.Body.String(), `"reachable":null`) || !strings.Contains(out.Body.String(), `"state":"unknown"`) {
			t.Fatal("UDP falsely confirmed", out.Body.String())
		}
	}
	for _, test := range []struct {
		body string
		code int
	}{{`{"id":2,"location":"public"}`, 200}, {`{"id":3,"location":"public"}`, 409}, {`{"id":99,"location":"local"}`, 404}, {`{"id":2,"location":"public","target_ip":"8.8.8.8"}`, 400}} {
		out := httptest.NewRecorder()
		s.portTestAPI(out, httptest.NewRequest("POST", "/api/port-test", strings.NewReader(test.body)))
		if out.Code != test.code {
			t.Fatal(out.Code, out.Body.String())
		}
		if test.code == 200 && !strings.Contains(out.Body.String(), `"reachable":false`) {
			t.Fatal("invalid Oracle IP accepted")
		}
	}
	if s.setupSnapshot().State != "waiting" {
		t.Fatal("empty setup status not explained")
	}
}
