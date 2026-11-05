package app

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var web embed.FS

type Service struct {
	cfg      Config
	db       *sql.DB
	auth     *auth
	traffic  traffic
	mu       sync.Mutex
	healthMu sync.RWMutex
	health   map[string]any
	started  time.Time
}

func newService(c Config) (*Service, error) {
	db, e := openDB(filepath.Join(c.Dir, "state.db"))
	if e != nil {
		return nil, e
	}
	s := &Service{cfg: c, db: db, auth: newAuth(db, c.Dir), traffic: traffic{data: map[string]counters{}}, health: map[string]any{}, started: time.Now()}
	if e = initAdmin(db, c.Dir); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Service) handlers() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("/api/login", s.auth.login)
	m.HandleFunc("/api/session", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie("__Host-ft")
		s.auth.mu.Lock()
		v := s.auth.sessions[c.Value]
		s.auth.mu.Unlock()
		jsonOut(w, 200, v)
	}))
	m.HandleFunc("/api/logout", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			fail(w, 405, "POST required")
			return
		}
		c, _ := r.Cookie("__Host-ft")
		s.auth.mu.Lock()
		delete(s.auth.sessions, c.Value)
		s.auth.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "__Host-ft", Path: "/", Secure: true, HttpOnly: true, MaxAge: -1})
		jsonOut(w, 200, map[string]bool{"ok": true})
	}))
	m.HandleFunc("/api/password", s.auth.protect(s.auth.password))
	m.HandleFunc("/api/routes", s.auth.protect(s.routeAPI))
	m.HandleFunc("/api/routes/", s.auth.protect(s.routeAPI))
	m.HandleFunc("/api/status", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			fail(w, 405, "GET required")
			return
		}
		s.healthMu.RLock()
		defer s.healthMu.RUnlock()
		jsonOut(w, 200, map[string]any{"role": s.cfg.Role, "public_ip": s.cfg.PublicIP, "uptime_seconds": int(time.Since(s.started).Seconds()), "active_connections": s.traffic.active.Load(), "health": s.health})
	}))
	m.HandleFunc("/api/stats", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			fail(w, 405, "GET required")
			return
		}
		s.flushStats()
		v, e := statsJSON(s.db, r.URL.Query().Get("period"))
		if e != nil {
			fail(w, 400, "invalid statistics request")
			return
		}
		jsonOut(w, 200, v)
	}))
	m.HandleFunc("/api/logs", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			fail(w, 405, "GET required")
			return
		}
		rows, e := s.db.Query("SELECT at,kind,message FROM events ORDER BY id DESC LIMIT 100")
		if e != nil {
			fail(w, 500, "database unavailable")
			return
		}
		defer rows.Close()
		out := []map[string]string{}
		for rows.Next() {
			var a, k, m string
			if rows.Scan(&a, &k, &m) == nil {
				out = append(out, map[string]string{"at": a, "kind": k, "message": m})
			}
		}
		jsonOut(w, 200, out)
	}))
	files, _ := fs.Sub(web, "web")
	m.Handle("/", http.FileServer(http.FS(files)))
	return security(m)
}
func (s *Service) routeAPI(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Role != "client" {
		fail(w, 409, "manage routes on the home client")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idText := strings.TrimPrefix(r.URL.Path, "/api/routes")
	idText = strings.Trim(idText, "/")
	test := strings.HasSuffix(idText, "/test")
	idText = strings.TrimSuffix(idText, "/test")
	var id int64
	var e error
	if idText != "" {
		id, e = strconv.ParseInt(idText, 10, 64)
		if e != nil || id < 1 {
			fail(w, 400, "invalid route id")
			return
		}
	}
	if test {
		if r.Method != "POST" {
			fail(w, 405, "POST required")
			return
		}
		rs, _ := routes(s.db)
		for _, v := range rs {
			if v.ID == id {
				jsonOut(w, 200, map[string]string{"backend": backendCheck(r.Context(), v)})
				return
			}
		}
		fail(w, 404, "route not found")
		return
	}
	if r.Method == "GET" {
		rs, e := routes(s.db)
		if e != nil {
			fail(w, 500, "database unavailable")
			return
		}
		jsonOut(w, 200, rs)
		return
	}
	var v Route
	if r.Method == "POST" || r.Method == "PUT" {
		if decode(w, r, &v) != nil {
			fail(w, 400, "invalid JSON")
			return
		}
		if e = v.validate(); e != nil {
			fail(w, 400, e.Error())
			return
		}
	}
	var result sql.Result
	switch r.Method {
	case "POST":
		if id != 0 {
			fail(w, 400, "use collection URL")
			return
		}
		result, e = s.db.Exec("INSERT INTO routes(domain,backend,enabled) VALUES(?,?,?)", v.Domain, v.Backend, v.Enabled)
	case "PUT":
		if id == 0 {
			fail(w, 400, "route id required")
			return
		}
		result, e = s.db.Exec("UPDATE routes SET domain=?,backend=?,enabled=? WHERE id=?", v.Domain, v.Backend, v.Enabled, id)
	case "DELETE":
		if id == 0 {
			fail(w, 400, "route id required")
			return
		}
		result, e = s.db.Exec("DELETE FROM routes WHERE id=?", id)
	default:
		fail(w, 405, "method not allowed")
		return
	}
	if e != nil {
		fail(w, 409, "change could not be saved; domain must be unique")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "route not found")
		return
	}
	event(s.db, "route", "route configuration changed")
	syncErr := s.syncRoutes()
	if syncErr != nil {
		jsonOut(w, 202, map[string]string{"message": "saved locally; server sync pending"})
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Service) internalHandlers() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("/internal/health", bearer(s.cfg.Token, func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, map[string]bool{"ok": true}) }))
	m.HandleFunc("/internal/routes", bearer(s.cfg.Token, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			fail(w, 405, "POST required")
			return
		}
		var rs []Route
		if decode(w, r, &rs) != nil {
			fail(w, 400, "invalid routes")
			return
		}
		if len(rs) > 200 {
			fail(w, 400, "too many routes")
			return
		}
		for _, v := range rs {
			if v.validate() != nil {
				fail(w, 400, "invalid route")
				return
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		old, e := routes(s.db)
		if e != nil {
			fail(w, 500, "database unavailable")
			return
		}
		if e = s.applyCaddy(rs); e != nil {
			fail(w, 503, e.Error())
			return
		}
		if e = replaceRoutes(s.db, rs); e != nil {
			_ = s.applyCaddy(old)
			fail(w, 500, "persist failed")
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	}))
	return m
}
func (s *Service) checkHealth(ctx context.Context) {
	h := map[string]any{}
	h["database"] = s.db.PingContext(ctx) == nil
	cmd := exec.CommandContext(ctx, "wg", "show", "ft0", "latest-handshakes")
	b, e := cmd.Output()
	if e != nil {
		h["wireguard"] = "inspection unavailable"
	} else {
		fresh := false
		parts := strings.Fields(string(b))
		for i := 1; i < len(parts); i += 2 {
			ts, _ := strconv.ParseInt(parts[i], 10, 64)
			if ts > 0 && time.Now().Unix()-ts < 180 {
				fresh = true
			}
		}
		if fresh {
			h["wireguard"] = "connected"
		} else {
			h["wireguard"] = "waiting for peer"
		}
	}
	if s.cfg.Role == "client" {
		s.mu.Lock()
		e = s.syncRoutes()
		s.mu.Unlock()
		if e != nil {
			h["server_sync"] = e.Error()
		} else {
			h["server_sync"] = "ok"
		}
	}
	if s.cfg.Role == "server" {
		s.mu.Lock()
		rs, _ := routes(s.db)
		e = s.applyCaddy(rs)
		s.mu.Unlock()
		h["caddy"] = e == nil
	}
	rs, _ := routes(s.db)
	checks := map[string]any{}
	for _, r := range rs {
		if !r.Enabled {
			continue
		}
		ips, e := net.DefaultResolver.LookupHost(ctx, r.Domain)
		dnsOK := false
		for _, ip := range ips {
			if ip == s.cfg.PublicIP {
				dnsOK = true
			}
		}
		v := map[string]any{"dns_matches_vps": e == nil && dnsOK}
		if s.cfg.Role == "client" {
			v["backend"] = backendCheck(ctx, r)
		}
		checks[r.Domain] = v
	}
	h["services"] = checks
	for _, p := range []string{"/proc/loadavg", "/proc/meminfo"} {
		if b, e := os.ReadFile(p); e == nil {
			h[filepath.Base(p)] = string(b)
		}
	}
	s.healthMu.Lock()
	s.health = h
	s.healthMu.Unlock()
}
func (s *Service) serve() error {
	host := hostOnly(s.cfg.Listen)
	if e := ensureTLS(s.cfg.Dir, host); e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	admin := &http.Server{Addr: s.cfg.Listen, Handler: s.handlers(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	var internal *http.Server
	if s.cfg.Role == "server" {
		internal = &http.Server{Addr: s.cfg.DataListen, Handler: s.internalHandlers(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second}
	} else {
		internal = &http.Server{Addr: s.cfg.DataListen, Handler: http.HandlerFunc(s.proxy), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 65536}
	}
	errs := make(chan error, 2)
	go func() { errs <- admin.ListenAndServeTLS(s.cfg.Dir+"/admin.crt", s.cfg.Dir+"/admin.key") }()
	go func() { errs <- internal.ListenAndServe() }()
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, 25*time.Second)
			s.checkHealth(check)
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.flushStats()
			}
		}
	}()
	event(s.db, "service", "controller started")
	var e error
	select {
	case <-ctx.Done():
	case e = <-errs:
		stop()
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = admin.Shutdown(shutdown)
	_ = internal.Shutdown(shutdown)
	s.flushStats()
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: familytunnel serve|status|repair|update|backup|restore|uninstall")
	}
	if args[0] == "version" {
		fmt.Println("0.1.0")
		return nil
	}
	if args[0] == "selftest" {
		if !verifyPassword(hashPassword("test password"), "test password") {
			return fmt.Errorf("crypto selftest failed")
		}
		fmt.Println("crypto selftest OK")
		return nil
	}
	dir := os.Getenv("FAMILYTUNNEL_DIR")
	if dir == "" {
		dir = "/var/lib/familytunnel"
	}
	if args[0] != "serve" {
		allowed := map[string]bool{"status": true, "repair": true, "update": true, "backup": true, "restore": true, "uninstall": true}
		if !allowed[args[0]] {
			return fmt.Errorf("unknown command")
		}
		cmd := exec.Command("/usr/local/lib/familytunnel/manage.sh", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	c, e := loadConfig(dir)
	if e != nil {
		return e
	}
	s, e := newService(c)
	if e != nil {
		return e
	}
	defer s.db.Close()
	return s.serve()
}
