package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type PortForward struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	TargetIP   string `json:"target_ip"`
	TargetPort int    `json:"target_port"`
	PublicPort int    `json:"public_port"`
	Enabled    bool   `json:"enabled"`
}

func (p PortForward) validate() error {
	if p.Protocol != "tcp" && p.Protocol != "udp" {
		return fmt.Errorf("TCP oder UDP auswählen")
	}
	if p.PublicPort < 1024 || p.PublicPort > 65535 || p.PublicPort == 2019 || p.PublicPort == 8443 || p.PublicPort == 9080 || p.PublicPort == 9081 || (p.Protocol == "udp" && p.PublicPort == 51820) {
		return fmt.Errorf("Öffentlicher Port muss 1024–65535 sein; Verwaltungsports und WireGuard sind reserviert")
	}
	if p.TargetPort < 1 || p.TargetPort > 65535 {
		return fmt.Errorf("Ungültiger interner Port")
	}
	ip := net.ParseIP(p.TargetIP)
	if ip == nil || ip.To4() == nil || !ip.IsPrivate() || ip.IsLoopback() || strings.HasPrefix(ip.String(), "10.77.0.") {
		return fmt.Errorf("Eine private lokale IPv4-Adresse eingeben")
	}
	if len(p.Name) > 80 {
		return fmt.Errorf("Name ist zu lang")
	}
	return nil
}

func portForwards(db *sql.DB) ([]PortForward, error) {
	rows, err := db.Query("SELECT id,name,protocol,target_ip,target_port,public_port,enabled FROM port_forwards ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PortForward{}
	for rows.Next() {
		var p PortForward
		if err = rows.Scan(&p.ID, &p.Name, &p.Protocol, &p.TargetIP, &p.TargetPort, &p.PublicPort, &p.Enabled); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func validateForwardSet(web []Route, ports []PortForward) error {
	if len(web) > 200 || len(ports) > 200 {
		return fmt.Errorf("Maximal 200 Webseiten und 200 Portfreigaben")
	}
	seen := map[string]bool{}
	for _, p := range ports {
		if err := p.validate(); err != nil {
			return err
		}
		key := p.Protocol + ":" + strconv.Itoa(p.PublicPort)
		if seen[key] {
			return fmt.Errorf("%s ist bereits vergeben", key)
		}
		seen[key] = true
		if p.Enabled && p.Protocol == "tcp" {
			for _, r := range web {
				if r.Enabled && r.port() == p.PublicPort {
					return fmt.Errorf("TCP-Port %d ist bereits einer HTTPS-Weiterleitung zugeordnet", p.PublicPort)
				}
			}
		}
	}
	return nil
}

func replaceAllRoutes(db *sql.DB, web []Route, ports []PortForward) error {
	if err := validateForwardSet(web, ports); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, r := range web {
		if err := r.validate(); err != nil {
			return err
		}
		if seen[r.address()] {
			return fmt.Errorf("Domain und Port doppelt")
		}
		seen[r.address()] = true
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM routes"); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM port_forwards"); err != nil {
		return err
	}
	for _, r := range web {
		if _, err = tx.Exec("INSERT INTO routes(id,domain,backend,enabled,public_port,name,backend_cert_sha256,backend_server_name) VALUES(?,?,?,?,?,?,?,?)", r.ID, r.Domain, r.Backend, r.Enabled, r.port(), r.Name, r.BackendCertSHA256, r.BackendServerName); err != nil {
			return err
		}
	}
	for _, p := range ports {
		if _, err = tx.Exec("INSERT INTO port_forwards(id,name,protocol,target_ip,target_port,public_port,enabled) VALUES(?,?,?,?,?,?,?)", p.ID, p.Name, p.Protocol, p.TargetIP, p.TargetPort, p.PublicPort, p.Enabled); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) notifyRoutes() {
	s.setSetup("working", "Auswahl gespeichert. Tunnel und Oracle-Freigaben werden eingerichtet.")
	select {
	case s.routeChanges <- struct{}{}:
	default:
	}
}

func (s *Service) portAPI(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Role != "client" {
		fail(w, 409, "Portfreigaben auf dem Heim-CT verwalten")
		return
	}
	if r.Method == "GET" {
		ps, err := portForwards(s.db)
		if err != nil {
			fail(w, 500, "Datenbank nicht verfügbar")
			return
		}
		jsonOut(w, 200, ps)
		return
	}
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	ps, err := portForwards(s.db)
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	web, err := routes(s.db)
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ports"), "/")
	next := int64(1)
	for _, p := range ps {
		if p.ID >= next {
			next = p.ID + 1
		}
	}
	switch {
	case r.Method == "POST" && (suffix == "import" || suffix == "batch"):
		var incoming []PortForward
		if decode(w, r, &incoming) != nil || len(incoming) > 200 {
			fail(w, 400, "Ungültige Importliste")
			return
		}
		for _, p := range incoming {
			p.Protocol = strings.ToLower(strings.TrimSpace(p.Protocol))
			p.TargetIP = strings.TrimSpace(p.TargetIP)
			p.Name = strings.TrimSpace(p.Name)
			if p.TargetPort == 0 {
				p.TargetPort = p.PublicPort
			}
			if suffix == "import" {
				p.Enabled = false
			}
			duplicate := false
			for _, old := range ps {
				if old.Protocol == p.Protocol && old.PublicPort == p.PublicPort {
					duplicate = true
					break
				}
			}
			if duplicate {
				if suffix == "batch" {
					fail(w, 409, "Protokoll und öffentlicher Port sind bereits vergeben")
					return
				}
				continue
			}
			p.ID = next
			next++
			ps = append(ps, p)
		}
	case r.Method == "POST" && suffix == "activate":
		var input struct {
			IDs []int64 `json:"ids"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Ungültige Auswahl")
			return
		}
		wanted := map[int64]bool{}
		for _, id := range input.IDs {
			wanted[id] = true
		}
		for i := range ps {
			if wanted[ps[i].ID] {
				ps[i].Enabled = true
				delete(wanted, ps[i].ID)
			}
		}
		if len(wanted) > 0 {
			fail(w, 404, "Freigabe nicht gefunden")
			return
		}
	case (r.Method == "POST" && suffix == "") || r.Method == "PUT":
		var p PortForward
		if decode(w, r, &p) != nil {
			fail(w, 400, "Ungültige Freigabe")
			return
		}
		p.Protocol = strings.ToLower(strings.TrimSpace(p.Protocol))
		p.TargetIP = strings.TrimSpace(p.TargetIP)
		p.Name = strings.TrimSpace(p.Name)
		if p.TargetPort == 0 {
			p.TargetPort = p.PublicPort
		}
		if p.Protocol == "both" {
			id := int64(0)
			if r.Method == "PUT" {
				id, err = strconv.ParseInt(suffix, 10, 64)
				if err != nil {
					fail(w, 400, "Ungültige ID")
					return
				}
			}
			ps, err = editBothPorts(ps, p, id)
			if err != nil {
				fail(w, 409, err.Error())
				return
			}
			break
		}
		if r.Method == "POST" {
			p.ID = next
			ps = append(ps, p)
		} else {
			id, e := strconv.ParseInt(suffix, 10, 64)
			if e != nil {
				fail(w, 400, "Ungültige ID")
				return
			}
			found := false
			for i := range ps {
				if ps[i].ID == id {
					p.ID = id
					ps[i] = p
					found = true
				}
			}
			if !found {
				fail(w, 404, "Freigabe nicht gefunden")
				return
			}
		}
	case r.Method == "DELETE":
		id, e := strconv.ParseInt(suffix, 10, 64)
		if e != nil {
			fail(w, 400, "Ungültige ID")
			return
		}
		filtered := []PortForward{}
		found := false
		for _, p := range ps {
			if p.ID == id {
				found = true
			} else {
				filtered = append(filtered, p)
			}
		}
		if !found {
			fail(w, 404, "Freigabe nicht gefunden")
			return
		}
		ps = filtered
	default:
		fail(w, 405, "Methode nicht erlaubt")
		return
	}
	if err = replaceAllRoutes(s.db, web, ps); err != nil {
		fail(w, 409, err.Error())
		return
	}
	event(s.db, "ports", "Portfreigaben geändert")
	s.notifyRoutes()
	jsonOut(w, 202, map[string]any{"ports": ps, "message": "Gespeichert. Tunnel- und Firewall-Konfiguration wird übertragen. Oracle-Freigabe wird separat geprüft."})
}

func (s *Service) configAPI(w http.ResponseWriter, r *http.Request) {
	// This authenticated internal endpoint runs from the background sync worker.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(120 * time.Second))
	ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if r.Method != "POST" || s.cfg.Role != "server" {
		fail(w, 405, "POST auf Oracle erforderlich")
		return
	}
	var config struct {
		Web   []Route       `json:"web"`
		Ports []PortForward `json:"ports"`
	}
	if decode(w, r, &config) != nil {
		fail(w, 400, "Ungültige Konfiguration")
		return
	}
	if err := validateForwardSet(config.Web, config.Ports); err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	oldWeb, err := routes(s.db)
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	oldPorts, err := portForwards(s.db)
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	if err = replaceAllRoutes(s.db, config.Web, config.Ports); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err = s.applyCaddy(config.Web); err == nil {
		err = s.applyPortFirewall(r.Context())
	}
	if err != nil {
		_ = replaceAllRoutes(s.db, oldWeb, oldPorts)
		_ = s.applyCaddy(oldWeb)
		_ = s.applyPortFirewall(context.Background())
		fail(w, 503, "Konfiguration zurückgerollt: "+err.Error())
		return
	}
	ready := s.ensureCloudTarget(r.Context())
	cloudMessage := "Einmalige Oracle-Verbindung fehlt. Die lokale Weiterleitung ist eingerichtet; die Cloud-Freigabe ist noch nicht bestätigt."
	if ready {
		if err = s.applyCloud(r.Context()); err != nil {
			ready = false
			cloudMessage = err.Error()
		} else {
			cloudMessage = "Tunnel und Oracle-Regeln eingerichtet. Die Erreichbarkeit des eigentlichen Dienstes bitte mit dem Dienst prüfen."
		}
	}
	jsonOut(w, 200, map[string]any{"ok": true, "cloud_ready": ready, "cloud_message": cloudMessage})
}

// Write only database configuration here; no arbitrary command execution endpoint exists.
func (s *Service) applyPortFirewall(ctx context.Context) error {
	if s.firewallApply == nil {
		return nil
	}
	return s.firewallApply(ctx)
}

func decodeSyncError(response *http.Response) error {
	var msg struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(response.Body).Decode(&msg)
	return fmt.Errorf("Oracle meldet HTTP %d: %s", response.StatusCode, msg.Error)
}
