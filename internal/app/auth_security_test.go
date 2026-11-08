package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoginSessionCookieCSRFAndExpiry(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = setSetting(db, "password", hashPassword("test-password")); err != nil {
		t.Fatal(err)
	}
	a := newAuth(db, t.TempDir())
	login := httptest.NewRecorder()
	a.login(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"test-password"}`)))
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	cookie := cookies[0]
	if cookie.Name != "__Host-ft" || !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie", cookie)
	}
	var data struct {
		CSRF string `json:"csrf"`
	}
	if err = json.Unmarshal(login.Body.Bytes(), &data); err != nil || len(data.CSRF) < 32 {
		t.Fatal("missing CSRF token")
	}
	protected := a.protect(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, test := range []struct {
		method, token string
		want          int
	}{{"GET", "", 204}, {"POST", "", 403}, {"DELETE", "wrong", 403}, {"POST", data.CSRF, 204}} {
		r := httptest.NewRequest(test.method, "/api/protected", nil)
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", test.token)
		out := httptest.NewRecorder()
		protected(out, r)
		if out.Code != test.want {
			t.Fatalf("%s: got %d want %d", test.method, out.Code, test.want)
		}
	}
	a.mu.Lock()
	entry := a.sessions[cookie.Value]
	entry.Until = time.Now().Add(-time.Second)
	a.sessions[cookie.Value] = entry
	a.mu.Unlock()
	r := httptest.NewRequest("GET", "/api/protected", nil)
	r.AddCookie(cookie)
	out := httptest.NewRecorder()
	protected(out, r)
	if out.Code != 401 {
		t.Fatal("expired session accepted")
	}
}

func TestLoginFailureRateLimitAndBearer(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = setSetting(db, "password", hashPassword("actual-password")); err != nil {
		t.Fatal(err)
	}
	a := newAuth(db, t.TempDir())
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		r.RemoteAddr = "192.0.2.10:12345"
		out := httptest.NewRecorder()
		a.login(out, r)
		want := 401
		if i == 5 {
			want = 429
		}
		if out.Code != want {
			t.Fatalf("attempt %d: %d", i, out.Code)
		}
	}
	h := bearer("private-test-token", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, test := range []struct {
		token string
		want  int
	}{{"", 401}, {"Bearer incorrect", 401}, {"Bearer private-test-token", 204}} {
		r := httptest.NewRequest("GET", "/internal/health", nil)
		r.Header.Set("Authorization", test.token)
		out := httptest.NewRecorder()
		h(out, r)
		if out.Code != test.want {
			t.Fatal("internal API authentication failed", out.Code)
		}
	}
}
