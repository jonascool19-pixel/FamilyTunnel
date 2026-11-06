package app

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"
)

func probeBackend(ctx context.Context, backend string) map[string]any {
	u, err := url.Parse(backend)
	if err != nil || u == nil || u.Hostname() == "" {
		return map[string]any{"reachable": false, "message": "Ungültige Zieladresse."}
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	c, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return map[string]any{"reachable": false, "message": "Vom CT nicht erreichbar: " + err.Error()}
	}
	c.Close()
	return map[string]any{"reachable": true, "message": "Vom CT erreichbar (TCP)."}
}

func (s *Service) backendTestAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "POST required")
		return
	}
	if s.cfg.Role != "client" {
		fail(w, 409, "Test auf dem Heim-Client ausführen")
		return
	}
	var input struct {
		Backend string `json:"backend"`
	}
	if decode(w, r, &input) != nil {
		fail(w, 400, "invalid JSON")
		return
	}
	if err := (Route{Domain: "test.example.net", Backend: input.Backend}).validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	result := probeBackend(r.Context(), input.Backend)
	event(s.db, "backend-test", input.Backend+": "+result["message"].(string))
	jsonOut(w, 200, result)
}
