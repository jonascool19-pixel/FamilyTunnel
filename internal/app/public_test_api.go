package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"
)

func publicProof(token, nonce, domain string) string {
	mac := hmac.New(sha256.New, []byte(token))
	fmt.Fprintf(mac, "%s\n%s", nonce, domain)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) publicTestAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, 405, "POST required")
		return
	}
	if s.cfg.Role != "client" {
		fail(w, 409, "Auf dem Heim-Client testen")
		return
	}
	var input struct {
		Domain string `json:"domain"`
	}
	if decode(w, r, &input) != nil || !domainRE.MatchString(input.Domain) {
		fail(w, 400, "Ungültige Domain")
		return
	}
	rs, err := routes(s.db)
	if err != nil {
		fail(w, 500, "database unavailable")
		return
	}
	enabled := false
	for _, route := range rs {
		if route.Domain == input.Domain && route.Enabled {
			enabled = true
		}
	}
	if !enabled {
		fail(w, 409, "Diesen Dienst zuerst veröffentlichen und aktivieren. Erst dann ist ein öffentlicher Test möglich.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result := map[string]any{"reachable": false, "message": ""}
	defer func() {
		event(s.db, "public-test", input.Domain+": "+result["message"].(string))
		jsonOut(w, 200, result)
	}()
	ips, err := net.DefaultResolver.LookupHost(ctx, input.Domain)
	matches := false
	for _, ip := range ips {
		if ip == s.cfg.PublicIP {
			matches = true
		}
	}
	if err != nil || !matches {
		result["message"] = "DNS-Fehler: Die Domain zeigt nicht auf die konfigurierte Oracle-IP " + s.cfg.PublicIP
		return
	}
	ip := net.ParseIP(s.cfg.PublicIP)
	if ip == nil || ip.IsPrivate() || ip.IsLoopback() || !ip.IsGlobalUnicast() {
		result["message"] = "Oracle-IP ist keine öffentliche IP."
		return
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(s.cfg.PublicIP, "443"))
	}, TLSHandshakeTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	nonce := random(32)
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+input.Domain+"/", nil)
	req.Header.Set("X-FamilyTunnel-Probe", nonce)
	response, err := client.Do(req)
	if err != nil {
		result["message"] = "Öffentlicher HTTPS-Test fehlgeschlagen (Port 443, Firewall oder Zertifikat): " + err.Error()
		return
	}
	defer response.Body.Close()
	if !hmac.Equal([]byte(response.Header.Get("X-FamilyTunnel-Proof")), []byte(publicProof(s.cfg.Token, nonce, input.Domain))) {
		result["message"] = fmt.Sprintf("HTTPS antwortet mit HTTP %d, aber der Weg über den Tunnel zum Heimdienst ist nicht bestätigt.", response.StatusCode)
		return
	}
	if response.StatusCode >= 500 {
		result["message"] = fmt.Sprintf("Öffentlicher Tunnel erreicht den Heimdienst, der aber HTTP %d meldet.", response.StatusCode)
		return
	}
	result["reachable"] = true
	result["message"] = fmt.Sprintf("Öffentlich erreichbar ✓: HTTPS → Oracle → WireGuard → Heimdienst bestätigt (HTTP %d).", response.StatusCode)
}
