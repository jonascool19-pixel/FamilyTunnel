package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type counters struct{ Requests, Errors, RX, TX int64 }
type countBody struct {
	io.ReadCloser
	n *atomic.Int64
}

func (b countBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.n.Add(int64(n))
	return n, e
}

type meterWriter struct {
	http.ResponseWriter
	status int
	n      int64
}

func (w *meterWriter) WriteHeader(n int) { w.status = n; w.ResponseWriter.WriteHeader(n) }
func (w *meterWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, e := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, e
}
func (w *meterWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *meterWriter) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }

type traffic struct {
	mu     sync.Mutex
	data   map[string]counters
	active atomic.Int64
}

func (t *traffic) add(domain string, c counters) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.data[domain]
	v.Requests += c.Requests
	v.Errors += c.Errors
	v.RX += c.RX
	v.TX += c.TX
	t.data[domain] = v
}
func (s *Service) flushStats() {
	s.traffic.mu.Lock()
	defer s.traffic.mu.Unlock()
	for key, c := range s.traffic.data {
		parts := strings.SplitN(key, "|", 2)
		_, e := s.db.Exec(`INSERT INTO stats(domain,hour,requests,errors,rx,tx) VALUES(?,?,?,?,?,?) ON CONFLICT(domain,hour) DO UPDATE SET requests=requests+excluded.requests,errors=errors+excluded.errors,rx=rx+excluded.rx,tx=tx+excluded.tx`, parts[0], parts[1], c.Requests, c.Errors, c.RX, c.TX)
		if e == nil {
			delete(s.traffic.data, key)
		}
	}
}

var backendTransport = &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second}

func (s *Service) proxy(w http.ResponseWriter, r *http.Request) {
	// WireGuard peer and source filtering prevent direct LAN/public use of the proxy.
	if hostOnly(r.RemoteAddr) != "10.77.0.1" {
		fail(w, 403, "WireGuard server only")
		return
	}
	rs, e := routes(s.db)
	if e != nil {
		fail(w, 503, "database unavailable")
		return
	}
	var target *Route
	for i := range rs {
		if rs[i].Enabled && rs[i].Domain == strings.ToLower(hostOnly(r.Host)) {
			target = &rs[i]
			break
		}
	}
	if target == nil {
		http.NotFound(w, r)
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		fail(w, 501, "WebSocket forwarding is not supported in this release")
		return
	}
	u, _ := url.Parse(target.Backend)
	p := httputil.NewSingleHostReverseProxy(u)
	p.Transport = backendTransport
	nonce := r.Header.Get("X-FamilyTunnel-Probe")
	p.ModifyResponse = func(response *http.Response) error {
		response.Header.Del("X-FamilyTunnel-Proof")
		if len(nonce) == 43 {
			response.Header.Set("X-FamilyTunnel-Proof", publicProof(s.cfg.Token, nonce, target.Domain))
		}
		return nil
	}
	original := p.Director
	p.Director = func(req *http.Request) {
		original(req)
		req.Header.Del("Authorization-Proxy")
		req.Header.Del("Proxy-Authorization")
		req.Header.Del("Forwarded")
		req.Header.Del("X-Forwarded-For")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Host = target.Domain
	}
	var rx atomic.Int64
	if r.Body != nil {
		r.Body = countBody{r.Body, &rx}
	}
	mw := &meterWriter{ResponseWriter: w}
	s.traffic.active.Add(1)
	defer s.traffic.active.Add(-1)
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { fail(w, 502, "backend unavailable") }
	p.ServeHTTP(mw, r)
	c := counters{Requests: 1, RX: rx.Load(), TX: mw.n}
	if mw.status >= 500 {
		c.Errors = 1
	}
	s.traffic.add(target.Domain+"|"+time.Now().UTC().Truncate(time.Hour).Format(time.RFC3339), c)
}
func caddyfile(rs []Route) string {
	var b strings.Builder
	b.WriteString("{\n admin 127.0.0.1:2019\n}\n")
	active := 0
	for _, r := range rs {
		if !r.Enabled {
			continue
		}
		active++
		fmt.Fprintf(&b, "%s {\n reverse_proxy 10.77.0.2:9080 {\n  transport http {\n   dial_timeout 5s\n   response_header_timeout 35s\n  }\n }\n log {\n  output discard\n }\n}\n", r.Domain)
	}
	if active == 0 {
		b.WriteString(":80 {\n respond \"FamilyTunnel: no services configured\" 404\n}\n")
	}
	return b.String()
}

var controlHTTP = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}

func (s *Service) applyCaddy(rs []Route) error {
	b := []byte(caddyfile(rs))
	req, e := http.NewRequest("POST", s.cfg.CaddyURL+"/load", bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "text/caddyfile")
	resp, e := controlHTTP.Do(req)
	if e != nil {
		return fmt.Errorf("Caddy unavailable")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 200 {
		return fmt.Errorf("Caddy rejected configuration (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
func (s *Service) syncRoutes() error {
	rs, e := routes(s.db)
	if e != nil {
		return e
	}
	b, _ := json.Marshal(rs)
	req, e := http.NewRequest("POST", s.cfg.ServerURL+"/internal/routes", bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	resp, e := controlHTTP.Do(req)
	if e != nil {
		return fmt.Errorf("server unreachable over WireGuard")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("server sync failed (%d)", resp.StatusCode)
	}
	return nil
}
func backendCheck(ctx context.Context, r Route) string {
	u, _ := url.Parse(r.Backend)
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	c, e := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if e != nil {
		return "unreachable"
	}
	c.Close()
	return "reachable"
}
