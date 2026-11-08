package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

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
		ID      int64  `json:"id"`
	}
	if decode(w, r, &input) != nil {
		fail(w, 400, "invalid JSON")
		return
	}
	target := Route{Domain: "test.example.net", Backend: input.Backend}
	if input.ID > 0 {
		web, e := routes(s.db)
		if e != nil {
			fail(w, 500, "Datenbank nicht verfügbar")
			return
		}
		found := false
		for _, v := range web {
			if v.ID == input.ID {
				target = v
				found = true
				break
			}
		}
		if !found {
			fail(w, 404, "Dienst nicht gefunden")
			return
		}
	}
	if err := target.validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	result := probeWebBackend(r.Context(), target)
	event(s.db, "backend-test", target.Backend+": "+result["message"].(string))
	jsonOut(w, 200, result)
}
func probeWebBackend(ctx context.Context, target Route) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	transport := backendNamedTransport(target.BackendCertSHA256, target.BackendServerName)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, "GET", target.Backend+"/", nil)
	if err != nil {
		return map[string]any{"reachable": false, "message": "Ungültiger Dienst"}
	}
	if target.BackendServerName != "" {
		req.Host = target.BackendServerName
	}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"reachable": false, "message": "HTTP/HTTPS vom CT nicht bestätigt: " + err.Error()}
	}
	resp.Body.Close()
	protocol := "HTTP"
	if strings.HasPrefix(target.Backend, "https://") {
		protocol = "HTTPS einschließlich TLS-Zertifikat"
	}
	return map[string]any{"reachable": resp.StatusCode < 500, "message": fmt.Sprintf("Vom CT geprüft: %s, HTTP-Status %d.", protocol, resp.StatusCode)}
}
