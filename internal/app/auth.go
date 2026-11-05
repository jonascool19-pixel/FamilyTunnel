package app

import (
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

func hashPassword(p string) string {
	s := random(16)
	h := argon2.IDKey([]byte(p), []byte(s), 3, 64*1024, 1, 32)
	return s + ":" + base64.RawStdEncoding.EncodeToString(h)
}
func verifyPassword(stored, p string) bool {
	parts := strings.Split(stored, ":")
	if len(parts) != 2 {
		return false
	}
	h := argon2.IDKey([]byte(p), []byte(parts[0]), 3, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare([]byte(base64.RawStdEncoding.EncodeToString(h)), []byte(parts[1])) == 1
}

type session struct {
	CSRF  string
	Until time.Time
}
type attempt struct {
	N     int
	Until time.Time
}
type auth struct {
	mu       sync.Mutex
	sessions map[string]session
	attempts map[string]attempt
	hashing  chan struct{}
	db       *sql.DB
	dir      string
}

func newAuth(db *sql.DB, dir string) *auth {
	return &auth{sessions: map[string]session{}, attempts: map[string]attempt{}, hashing: make(chan struct{}, 1), db: db, dir: dir}
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	jsonOut(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func (a *auth) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, 405, "POST required")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decode(w, r, &in) != nil || len(in.Password) > 1024 {
		fail(w, 400, "invalid request")
		return
	}
	ip := hostOnly(r.RemoteAddr)
	a.mu.Lock()
	now := time.Now()
	for k, v := range a.attempts {
		if now.After(v.Until) {
			delete(a.attempts, k)
		}
	}
	v := a.attempts[ip]
	if v.N >= 5 || len(a.attempts) > 4096 {
		a.mu.Unlock()
		fail(w, 429, "try again in 15 minutes")
		return
	}
	v.N++
	if v.Until.IsZero() {
		v.Until = now.Add(15 * time.Minute)
	}
	a.attempts[ip] = v
	a.mu.Unlock()
	select {
	case a.hashing <- struct{}{}:
		defer func() { <-a.hashing }()
	default:
		fail(w, 429, "login busy")
		return
	}
	ok := verifyPassword(setting(a.db, "password"), in.Password)
	if in.Username != "admin" || !ok {
		fail(w, 401, "invalid credentials")
		return
	}
	id := random(32)
	s := session{random(32), now.Add(8 * time.Hour)}
	a.mu.Lock()
	for k, v := range a.sessions {
		if now.After(v.Until) {
			delete(a.sessions, k)
		}
	}
	if len(a.sessions) >= 64 {
		a.mu.Unlock()
		fail(w, 429, "session limit")
		return
	}
	a.sessions[id] = s
	delete(a.attempts, ip)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "__Host-ft", Value: id, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
	jsonOut(w, 200, map[string]string{"csrf": s.CSRF})
}
func (a *auth) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("__Host-ft")
		if e != nil {
			fail(w, 401, "login required")
			return
		}
		a.mu.Lock()
		s, ok := a.sessions[c.Value]
		if time.Now().After(s.Until) {
			delete(a.sessions, c.Value)
			ok = false
		}
		a.mu.Unlock()
		if !ok {
			fail(w, 401, "session expired")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) != 1 {
			fail(w, 403, "CSRF token required")
			return
		}
		next(w, r)
	}
}
func (a *auth) password(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, 405, "POST required")
		return
	}
	var in struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if decode(w, r, &in) != nil || len(in.Password) < 6 || len(in.Password) > 256 || len(in.Current) > 1024 {
		fail(w, 400, "Das neue Passwort muss mindestens 6 und höchstens 256 Zeichen haben.")
		return
	}
	select {
	case a.hashing <- struct{}{}:
		defer func() { <-a.hashing }()
	default:
		fail(w, 429, "busy")
		return
	}
	if !verifyPassword(setting(a.db, "password"), in.Current) {
		fail(w, 403, "wrong current password")
		return
	}
	if e := setSetting(a.db, "password", hashPassword(in.Password)); e != nil {
		fail(w, 500, "password storage failed")
		return
	}
	a.mu.Lock()
	a.sessions = map[string]session{}
	a.mu.Unlock()
	_ = os.Remove(filepath.Join(a.dir, "initial-password"))
	jsonOut(w, 200, map[string]string{"message": "password changed; log in again"})
}
func bearer(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			fail(w, 401, "unauthorized")
			return
		}
		next(w, r)
	}
}
func security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}
func initAdmin(db *sql.DB, dir string) error {
	if setting(db, "password") != "" {
		return nil
	}
	p := random(24)
	if e := writeAtomic(dir+"/initial-password", []byte(fmt.Sprintln(p)), 0600); e != nil {
		return e
	}
	return setSetting(db, "password", hashPassword(p))
}
