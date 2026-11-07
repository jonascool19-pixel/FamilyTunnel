package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPrivateHTTPSDetectionAndPinVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`<title>Jellyfin</title>`)) }))
	defer server.Close()
	ip, number, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	port, _ := strconv.Atoi(number)
	name, backend, pin := probeLANServiceInfo(context.Background(), ip, port, true)
	sum := sha256.Sum256(server.Certificate().Raw)
	if name != "Jellyfin" || backend != server.URL || pin != hex.EncodeToString(sum[:]) {
		t.Fatal("HTTPS not identified and bound", name, backend, pin)
	}
	transport := backendHTTPTransport(pin)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	wrong := backendHTTPTransport(strings.Repeat("0", 64))
	defer wrong.CloseIdleConnections()
	client.Transport = wrong
	if response, err = client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("changed certificate accepted")
	}
}

func TestManualAutoSetupConvertsDirectHTTPSAndPreservesPins(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pin := strings.Repeat("a", 64)
	s := &Service{db: db, cfg: Config{Role: "client"}, routeChanges: make(chan struct{}, 1), probeService: func(ctx context.Context, ip string, port int) (string, string, string) {
		if port == 8920 {
			return "Jellyfin", "https://" + ip + ":8920", pin
		}
		return "Webdienst", "http://" + ip + ":" + strconv.Itoa(port), ""
	}}
	if err = replaceAllRoutes(db, []Route{}, []PortForward{{ID: 1, Protocol: "tcp", TargetIP: "192.168.1.95", TargetPort: 8920, PublicPort: 8920, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	input := `{"target_ip":"192.168.1.95","ports":"8920,8096","name":"Mein Medienserver","domain":"public.example.net","protocol":"auto"}`
	for i := 0; i < 2; i++ {
		out := httptest.NewRecorder()
		s.manualServiceAPI(out, httptest.NewRequest("POST", "/api/services/manual", strings.NewReader(input)))
		if out.Code != 202 {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	web, _ := routes(db)
	direct, _ := portForwards(db)
	if len(web) != 2 || len(direct) != 0 || web[0].Name != "Mein Medienserver" {
		t.Fatalf("manual setup not applied %+v %+v", web, direct)
	}
	if web[0].Backend != "https://192.168.1.95:8920" || web[0].BackendCertSHA256 != pin || web[1].Backend != "http://192.168.1.95:8096" || web[1].BackendCertSHA256 != "" {
		t.Fatal("protocols or certificate binding mixed")
	}
	clone, err := openDB(filepath.Join(t.TempDir(), "clone"))
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	if err = replaceAllRoutes(clone, web, direct); err != nil {
		t.Fatal(err)
	}
	saved, _ := routes(clone)
	if saved[0].BackendCertSHA256 != pin {
		t.Fatal("sync lost TLS pin")
	}
	s.probeService = func(context.Context, string, int) (string, string, string) {
		t.Fatal("public address probed")
		return "", "", ""
	}
	out := httptest.NewRecorder()
	s.manualServiceAPI(out, httptest.NewRequest("POST", "/api/services/manual", strings.NewReader(`{"target_ip":"8.8.8.8","ports":"8096"}`)))
	if out.Code != 400 {
		t.Fatal("public target accepted")
	}
}
