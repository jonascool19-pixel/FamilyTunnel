package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicProofBinding(t *testing.T) {
	proof := publicProof("secret", "nonce", "test.example.net")
	if proof == publicProof("other", "nonce", "test.example.net") || proof == publicProof("secret", "other", "test.example.net") || proof == publicProof("secret", "nonce", "other.example.net") {
		t.Fatal("proof must bind token, nonce and domain")
	}
}

func TestProxyProofRequiresBackendResponse(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer backend.Close()
	db, err := openDB(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("INSERT INTO routes(domain,backend,enabled) VALUES(?,?,1)", "test.example.net", backend.URL); err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db, cfg: Config{Token: "secret"}, traffic: traffic{data: map[string]counters{}}}
	request := httptest.NewRequest("GET", "http://test.example.net/", nil)
	request.RemoteAddr = "10.77.0.1:12345"
	nonce := strings.Repeat("a", 43)
	request.Header.Set("X-FamilyTunnel-Probe", nonce)
	recorder := httptest.NewRecorder()
	s.proxy(recorder, request)
	if recorder.Code != 401 || recorder.Header().Get("X-FamilyTunnel-Proof") != publicProof("secret", nonce, "test.example.net") {
		t.Fatalf("backend response not confirmed: %d %v", recorder.Code, recorder.Header())
	}
	backend.Close()
	recorder = httptest.NewRecorder()
	s.proxy(recorder, request)
	if recorder.Code != 502 || recorder.Header().Get("X-FamilyTunnel-Proof") != "" {
		t.Fatal("unreachable backend must not produce proof")
	}
}

func TestPublicTestRequiresPublishedRoute(t *testing.T) {
	db, err := openDB(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{cfg: Config{Role: "client"}, db: db}
	req := httptest.NewRequest(http.MethodPost, "/api/public-test", strings.NewReader(`{"domain":"test.example.net"}`))
	response := httptest.NewRecorder()
	s.publicTestAPI(response, req)
	if response.Code != 409 {
		t.Fatalf("unpublished test: %d %s", response.Code, response.Body.String())
	}
}
