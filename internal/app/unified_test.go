package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestServiceNamesAndSafeRedirects(t *testing.T) {
	var escaped atomic.Int32
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escaped.Add(1) }))
	defer outside.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/web/", 302)
			return
		}
		if r.URL.Path == "/web/" {
			w.Write([]byte(`<title> Jellyfin &amp; Musik </title>`))
			return
		}
	}))
	defer server.Close()
	ip, number, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(number)
	name, backend := probeLANService(context.Background(), ip, port)
	if name != "Jellyfin & Musik" || backend != server.URL {
		t.Fatalf("missing identification: %s %s", name, backend)
	}
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, outside.URL, 302) }))
	defer external.Close()
	ip, number, _ = net.SplitHostPort(strings.TrimPrefix(external.URL, "http://"))
	port, _ = strconv.Atoi(number)
	_, _ = probeLANService(context.Background(), ip, port)
	if escaped.Load() != 0 {
		t.Fatal("probe escaped to another endpoint")
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`<title>Untrusted</title>`)) }))
	defer tlsServer.Close()
	ip, number, _ = net.SplitHostPort(strings.TrimPrefix(tlsServer.URL, "https://"))
	port, _ = strconv.Atoi(number)
	_, backend = probeLANService(context.Background(), ip, port)
	if backend != "" {
		t.Fatal("untrusted HTTPS certificate treated as verified backend")
	}
}

func TestUnifiedPublicationAtomicPersistentAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db, cfg: Config{Role: "client"}, routeChanges: make(chan struct{}, 1)}
	for _, item := range []struct {
		ip            string
		port          int
		name, backend string
	}{{"192.168.1.95", 8096, "Jellyfin", "http://192.168.1.95:8096"}, {"192.168.1.40", 80, "Musikboard", "http://192.168.1.40:80"}, {"192.168.1.29", 7777, "TCP-Dienst 7777", ""}} {
		_, err = db.Exec("INSERT INTO scan_results(target_ip,target_port,protocol,found_at) VALUES(?,?,'tcp','now')", item.ip, item.port)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec("INSERT INTO scan_services VALUES(?,?,?,?)", item.ip, item.port, item.name, item.backend)
		if err != nil {
			t.Fatal(err)
		}
	}
	input := `[{"target_ip":"192.168.1.95","target_port":8096,"protocol":"tcp","auto":true,"domain":"public.example.net"},{"target_ip":"192.168.1.40","target_port":80,"protocol":"tcp","auto":true,"domain":"public.example.net"},{"target_ip":"192.168.1.29","target_port":7777,"protocol":"tcp","auto":true}]`
	// Without the domain, the entire mixed selection must remain unpublished.
	failed := httptest.NewRecorder()
	s.publishPortsAPI(failed, httptest.NewRequest("POST", "/api/ports/publish", strings.NewReader(strings.ReplaceAll(input, `,"domain":"public.example.net"`, ""))))
	if failed.Code != 400 {
		t.Fatal("missing domain not rejected")
	}
	web, _ := routes(db)
	if len(web) != 0 {
		t.Fatal("partial batch committed")
	}
	for i := 0; i < 2; i++ {
		out := httptest.NewRecorder()
		s.publishPortsAPI(out, httptest.NewRequest("POST", "/api/ports/publish", strings.NewReader(input)))
		if out.Code != 202 {
			t.Fatal(out.Body.String())
		}
		var response map[string]any
		_ = json.Unmarshal(out.Body.Bytes(), &response)
		if len(response["routes"].([]any)) != 2 {
			t.Fatal("HTTPS sites not returned immediately")
		}
	}
	web, _ = routes(db)
	ports, _ := portForwards(db)
	if len(web) != 2 || len(ports) != 1 || web[0].port() != 8096 || web[1].port() != 443 || ports[0].TargetPort != 7777 {
		t.Fatalf("wrong combined publication: %+v %+v", web, ports)
	}
	db.Close()
	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.db = db
	if s.discoveryDomain() != "public.example.net" {
		t.Fatal("domain not retained")
	}
	out := httptest.NewRecorder()
	s.scanAPI(out, httptest.NewRequest("GET", "/api/scan", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Musikboard") || !strings.Contains(out.Body.String(), "Jellyfin") {
		t.Fatal("service names lost on reopen")
	}
}

func TestUnifiedPortCollisionAndExistingDirectService(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db, cfg: Config{Role: "client"}}
	_, _ = db.Exec("INSERT INTO scan_services VALUES('192.168.1.95',8096,'Jellyfin','http://192.168.1.95:8096')")
	_, _ = db.Exec("INSERT INTO scan_services VALUES('192.168.1.40',8096,'Musikboard','http://192.168.1.40:8096')")
	// A web and raw target requesting the same external port must both be kept.
	input := `[{"target_ip":"192.168.1.95","target_port":8096,"protocol":"tcp","auto":true,"domain":"public.example.net"},{"target_ip":"192.168.1.29","target_port":8096,"protocol":"tcp","auto":true},{"target_ip":"192.168.1.40","target_port":8096,"protocol":"tcp","auto":true,"domain":"public.example.net"}]`
	out := httptest.NewRecorder()
	s.publishPortsAPI(out, httptest.NewRequest("POST", "/api/ports/publish", strings.NewReader(input)))
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	web, _ := routes(db)
	ports, _ := portForwards(db)
	if len(web) != 2 || len(ports) != 1 || web[0].port() != 8096 || ports[0].PublicPort != 8097 || web[1].port() != 8098 {
		t.Fatalf("unsafe allocation: %+v %+v", web, ports)
	}
	// Detecting HTTP later must not silently convert a running direct service.
	_, _ = db.Exec("INSERT INTO scan_services VALUES('192.168.1.29',8096,'Existing','http://192.168.1.29:8096')")
	out = httptest.NewRecorder()
	s.publishPortsAPI(out, httptest.NewRequest("POST", "/api/ports/publish", strings.NewReader(input)))
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	after, _ := portForwards(db)
	if len(after) != 1 || after[0].PublicPort != 8097 {
		t.Fatal("existing direct service replaced")
	}
}
