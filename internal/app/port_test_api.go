package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Service) portTestAPI(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Role != "client" || r.Method != "POST" {
		fail(w, 405, "POST auf dem Heim-CT erforderlich")
		return
	}
	var input struct {
		ID       int64  `json:"id"`
		Location string `json:"location"`
		Domain   string `json:"domain"`
	}
	if decode(w, r, &input) != nil || (input.Location != "local" && input.Location != "public") {
		fail(w, 400, "Heimnetz oder öffentlich auswählen")
		return
	}
	ps, err := portForwards(s.db)
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	var p *PortForward
	for i := range ps {
		if ps[i].ID == input.ID {
			p = &ps[i]
			break
		}
	}
	if p == nil {
		fail(w, 404, "Freigabe nicht gefunden")
		return
	}
	if err = p.validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if input.Location == "public" && !p.Enabled {
		fail(w, 409, "Dienst zuerst aktivieren")
		return
	}
	result := map[string]any{"state": "unknown", "reachable": nil, "checked_at": time.Now().UTC().Format(time.RFC3339)}
	defer func() {
		event(s.db, "port-test", fmt.Sprintf("%s %s/%d: %s", input.Location, p.Protocol, p.PublicPort, result["message"]))
		jsonOut(w, 200, result)
	}()
	if p.Protocol == "udp" {
		result["message"] = "UDP ist ohne passende Antwort des Spiels/Dienstes nicht zuverlässig prüfbar. Ein offener UDP-Port wird hier nicht behauptet."
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	ip, port := p.TargetIP, p.TargetPort
	if input.Location == "public" {
		ip, port = s.cfg.PublicIP, p.PublicPort
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.IsPrivate() || parsed.IsLoopback() || !parsed.IsGlobalUnicast() {
			result["state"] = "failed"
			result["reachable"] = false
			result["message"] = "Keine gültige öffentliche Oracle-IP konfiguriert."
			return
		}
		domain := strings.ToLower(strings.TrimSpace(input.Domain))
		if domain != "" {
			if len(domain) > 253 || !domainRE.MatchString(domain) {
				result["state"] = "failed"
				result["reachable"] = false
				result["message"] = "Ungültige öffentliche Domain."
				return
			}
			ips, e := net.DefaultResolver.LookupHost(ctx, domain)
			matches := false
			for _, address := range ips {
				if address == ip {
					matches = true
				}
			}
			if e != nil || !matches {
				result["state"] = "failed"
				result["reachable"] = false
				result["message"] = "DNS-Fehler: Die Domain zeigt nicht auf die konfigurierte Oracle-IP " + ip
				return
			}
		}
	}
	connection, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		result["state"] = "failed"
		result["reachable"] = false
		result["message"] = "TCP-Verbindung zu " + net.JoinHostPort(ip, strconv.Itoa(port)) + " fehlgeschlagen: " + err.Error()
		return
	}
	connection.Close()
	result["state"] = "reachable"
	result["reachable"] = true
	if input.Location == "local" {
		result["message"] = "Vom Heim-CT erreichbar (TCP): " + net.JoinHostPort(ip, strconv.Itoa(port))
	} else {
		result["message"] = "Öffentliche TCP-Verbindung zur Oracle-Adresse " + net.JoinHostPort(ip, strconv.Itoa(port)) + " möglich. Die Funktion des Dienstes ist damit noch nicht bestätigt."
	}
}
