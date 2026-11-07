package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBothProtocolsEditIsAtomicAndDoesNotLeaveOldPorts(t *testing.T) {
	s := maintenanceTestService(t, "client")
	if err := replaceAllRoutes(s.db, nil, []PortForward{{ID: 1, Name: "game", Protocol: "tcp", TargetIP: "192.168.1.29", TargetPort: 7777, PublicPort: 7777, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	put := func(payload string) *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		s.portAPI(out, httptest.NewRequest("PUT", "/api/ports/1", strings.NewReader(payload)))
		return out
	}
	out := put(`{"protocol":"both","target_ip":"192.168.1.29","target_port":7777,"public_port":7777,"enabled":true}`)
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	out = put(`{"protocol":"both","target_ip":"192.168.1.30","target_port":8888,"public_port":8889,"enabled":true}`)
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	ps, _ := portForwards(s.db)
	if len(ps) != 2 {
		t.Fatal(ps)
	}
	for _, p := range ps {
		if p.TargetIP != "192.168.1.30" || p.PublicPort != 8889 || p.TargetPort != 8888 {
			t.Fatalf("stale companion: %+v", ps)
		}
	}
	ps = append(ps, PortForward{ID: 9, Protocol: "udp", TargetIP: "192.168.1.40", TargetPort: 9999, PublicPort: 9999, Enabled: true})
	if err := replaceAllRoutes(s.db, nil, ps); err != nil {
		t.Fatal(err)
	}
	out = put(`{"protocol":"both","target_ip":"192.168.1.30","target_port":8888,"public_port":9999,"enabled":true}`)
	if out.Code != 409 {
		t.Fatal("conflicting UDP companion accepted")
	}
	after, _ := portForwards(s.db)
	if fmt.Sprint(after) != fmt.Sprint(ps) {
		t.Fatal("failed edit partially changed ports")
	}
}
func TestWebAndUDPCompanionSaveTogether(t *testing.T) {
	s := maintenanceTestService(t, "client")
	save := func(body string) *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		s.webServiceEditAPI(out, httptest.NewRequest("POST", "/api/services/web-edit", strings.NewReader(body)))
		return out
	}
	body := `{"route":{"name":"media","domain":"media.example.net","backend":"https://192.168.1.95:8920","public_port":8920,"enabled":true},"udp":true}`
	out := save(body)
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	web, _ := routes(s.db)
	ps, _ := portForwards(s.db)
	if len(web) != 1 || len(ps) != 1 || ps[0].Protocol != "udp" || ps[0].PublicPort != 8920 {
		t.Fatal(web, ps)
	}
	body = `{"route":{"id":1,"name":"media","domain":"media.example.net","backend":"https://192.168.1.96:8921","public_port":8922,"enabled":true},"udp":true}`
	out = save(body)
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	web, _ = routes(s.db)
	ps, _ = portForwards(s.db)
	if len(web) != 1 || len(ps) != 1 || ps[0].TargetIP != "192.168.1.96" || ps[0].TargetPort != 8921 || ps[0].PublicPort != 8922 {
		t.Fatal(web, ps)
	}
	out = save(strings.Replace(body, `"udp":true`, `"udp":false`, 1))
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	ps, _ = portForwards(s.db)
	if len(ps) != 0 {
		t.Fatal("UDP was not removed")
	}
}
func TestSlowHTTPSIsNotMisclassifiedAsRawTCP(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1100 * time.Millisecond)
		fmt.Fprint(w, "<title>Slow private HTTPS</title>")
	}))
	server.TLS = &tls.Config{GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) { time.Sleep(800 * time.Millisecond); return nil, nil }}
	server.StartTLS()
	defer server.Close()
	ip, p, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	port, _ := strconv.Atoi(p)
	name, backend, pin := probeLANServiceInfo(context.Background(), ip, port, true)
	if name != "Slow private HTTPS" || backend != server.URL || len(pin) != 64 {
		t.Fatalf("HTTPS not identified: %q %q %q", name, backend, pin)
	}
}
func TestHTTPSWithRequiredServerNameAndPinnedProxy(t *testing.T) {
	const domain = "media.example.net"
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<title>Named HTTPS</title>") }))
	server.Config.ErrorLog = nil
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		if hello.ServerName != domain {
			return nil, fmt.Errorf("server name required")
		}
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()
	ip, p, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	port, _ := strconv.Atoi(p)
	_, wrong, _ := probeLANServiceInfo(context.Background(), ip, port, true)
	if wrong != "" {
		t.Fatal("IP-only TLS probe unexpectedly succeeded")
	}
	name, backend, pin, sni := probeLANServiceNamed(context.Background(), ip, port, true, domain)
	if name != "Named HTTPS" || backend != server.URL || sni != domain || len(pin) != 64 {
		t.Fatal(name, backend, sni, pin)
	}
	transport := backendNamedTransport(pin, sni)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	resp, e := client.Get(backend)
	if e != nil {
		t.Fatal("named pinned backend failed:", e)
	}
	resp.Body.Close()
	s := maintenanceTestService(t, "client")
	r := Route{ID: 1, Domain: domain, Backend: backend, Enabled: true, PublicPort: 8920, BackendCertSHA256: pin, BackendServerName: sni}
	// Store private production-style address; test listener is loopback and is
	// deliberately never accepted by Route.validate for a published service.
	r.Backend = "https://192.168.1.95:8920"
	if e = replaceAllRoutes(s.db, []Route{r}, nil); e != nil {
		t.Fatal(e)
	}
	saved, _ := routes(s.db)
	if len(saved) != 1 || saved[0].BackendServerName != domain {
		t.Fatal("SNI did not persist", saved)
	}
}
func TestOptionalLiveNamedHTTPS(t *testing.T) {
	ip, domain := os.Getenv("FT_TEST_LAN_IP"), os.Getenv("FT_TEST_LAN_DOMAIN")
	if ip == "" || domain == "" {
		t.Skip("explicit opt-in LAN diagnostic")
	}
	parsed := net.ParseIP(ip)
	if parsed == nil || !parsed.IsPrivate() || !domainRE.MatchString(domain) {
		t.Fatal("private IP and valid domain required")
	}
	port, e := strconv.Atoi(os.Getenv("FT_TEST_LAN_PORT"))
	if e != nil || port < 1 || port > 65535 {
		t.Fatal("invalid diagnostic port")
	}
	name, backend, pin, sni := probeLANServiceNamed(context.Background(), ip, port, true, domain)
	if !strings.HasPrefix(backend, "https://") || pin == "" || sni != domain {
		t.Fatal("named HTTPS not confirmed", name, backend, sni)
	}
	transport := backendNamedTransport(pin, sni)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Get(backend + "/")
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	checked := probeWebBackend(context.Background(), Route{Backend: backend, BackendCertSHA256: pin, BackendServerName: sni})
	if checked["reachable"] != true {
		t.Fatal("HTTP/HTTPS health check failed", checked)
	}
	t.Log("Named LAN HTTPS and certificate-pinned backend connection confirmed", resp.StatusCode)
}
