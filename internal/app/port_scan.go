package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type scanJob struct {
	Running  bool   `json:"running"`
	Total    int    `json:"total"`
	Done     int    `json:"done"`
	Found    int    `json:"found"`
	Started  string `json:"started"`
	Message  string `json:"message"`
	Targets  string `json:"targets"`
	Mode     string `json:"mode"`
	cancel   context.CancelFunc
	finished chan struct{}
}

var quickPorts = []int{22, 80, 443, 1337, 2333, 3000, 3001, 5000, 7777, 8000, 8080, 8081, 8082, 8083, 8096, 8123, 8888, 8920, 9000, 10823, 15777, 26900, 26905, 27015, 27016, 27020, 30814, 32400, 57000}

func scanPlan(admin, targets, mode string) ([]string, []int, error) {
	allowed, err := scanIPv4s(admin)
	if err != nil {
		return nil, nil, err
	}
	ips := allowed
	if strings.TrimSpace(targets) != "" {
		ips = []string{}
		seen := map[string]bool{}
		for _, word := range strings.FieldsFunc(targets, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' }) {
			valid := false
			for _, ip := range allowed {
				if word == ip {
					valid = true
					break
				}
			}
			if !valid {
				return nil, nil, fmt.Errorf("Nur Geräte im gleichen privaten /24-Heimnetz wie der CT durchsuchen")
			}
			if !seen[word] {
				ips = append(ips, word)
				seen[word] = true
			}
		}
	}
	ports := quickPorts
	if mode == "full" {
		ports = make([]int, 65535)
		for i := range ports {
			ports[i] = i + 1
		}
	} else if mode != "quick" {
		return nil, nil, fmt.Errorf("Schnell oder vollständig auswählen")
	}
	return ips, ports, nil
}

func (s *Service) scanSnapshot() map[string]any {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	job := scanJob{}
	if s.scan != nil {
		job.Running = s.scan.Running
		job.Total = s.scan.Total
		job.Done = s.scan.Done
		job.Found = s.scan.Found
		job.Started = s.scan.Started
		job.Message = s.scan.Message
		job.Targets = s.scan.Targets
		job.Mode = s.scan.Mode
	} else {
		_ = json.Unmarshal([]byte(setting(s.db, "last_port_scan")), &job)
		if job.Running {
			job.Running = false
			job.Message = "Suche durch Dienstneustart beendet. Gespeicherte Treffer bleiben erhalten."
		}
	}
	return map[string]any{"running": job.Running, "total": job.Total, "done": job.Done, "found": job.Found, "started": job.Started, "message": job.Message, "targets": job.Targets, "mode": job.Mode}
}

func (s *Service) stopScan() {
	s.scanMu.Lock()
	job := s.scan
	if job != nil && job.Running {
		job.cancel()
	}
	s.scanMu.Unlock()
	if job != nil && job.finished != nil {
		<-job.finished
	}
}

func (s *Service) runPortScan(ctx context.Context, job *scanJob, ips []string, ports []int) {
	defer close(job.finished)
	type target struct {
		ip   string
		port int
	}
	tasks := make(chan target)
	webProbes := make(chan struct{}, 8)
	serverName := s.discoveryDomain()
	var workers sync.WaitGroup
	for i := 0; i < 64; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for t := range tasks {
				if ctx.Err() != nil {
					return
				}
				conn, err := (&net.Dialer{Timeout: 250 * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort(t.ip, strconv.Itoa(t.port)))
				if err == nil {
					conn.Close()
					select {
					case webProbes <- struct{}{}:
					case <-ctx.Done():
						return
					}
					name, backend, pin, tlsName := probeLANServiceNamed(ctx, t.ip, t.port, true, serverName)
					<-webProbes
					s.routeMu.Lock()
					result, e := s.db.Exec("INSERT INTO scan_results(target_ip,target_port,protocol,found_at) SELECT ?,?,'tcp',? WHERE (SELECT count(*) FROM scan_results)<4096 ON CONFLICT(target_ip,target_port,protocol) DO UPDATE SET found_at=excluded.found_at", t.ip, t.port, time.Now().UTC().Format(time.RFC3339))
					_, _ = s.db.Exec("INSERT INTO scan_services(target_ip,target_port,name,backend,backend_cert_sha256,backend_server_name) SELECT ?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM scan_results WHERE target_ip=? AND target_port=? AND protocol='tcp') ON CONFLICT(target_ip,target_port) DO UPDATE SET name=excluded.name,backend=excluded.backend,backend_cert_sha256=excluded.backend_cert_sha256,backend_server_name=excluded.backend_server_name", t.ip, t.port, name, backend, pin, tlsName, t.ip, t.port)
					p := PortForward{Name: name, Protocol: "tcp", TargetIP: t.ip, TargetPort: t.port, PublicPort: t.port}
					if p.validate() == nil {
						_, _ = s.db.Exec("INSERT INTO port_forwards(name,protocol,target_ip,target_port,public_port,enabled) SELECT ?,?,?,?,?,0 WHERE (SELECT count(*) FROM port_forwards)<200 ON CONFLICT(protocol,public_port) DO NOTHING", p.Name, p.Protocol, p.TargetIP, p.TargetPort, p.PublicPort)
					}
					s.routeMu.Unlock()
					if e == nil && result != nil {
						s.scanMu.Lock()
						job.Found++
						s.scanMu.Unlock()
					}
				}
				s.scanMu.Lock()
				job.Done++
				s.scanMu.Unlock()
			}
		}()
	}
	go func() {
		defer close(tasks)
		for _, ip := range ips {
			for _, port := range ports {
				select {
				case tasks <- target{ip, port}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	workers.Wait()
	s.scanMu.Lock()
	job.Running = false
	job.Message = "TCP-Suche abgeschlossen. Treffer sind gespeichert; Vorschläge sind noch nicht öffentlich aktiv. UDP wurde nicht als offen bestätigt."
	if ctx.Err() != nil {
		job.Message = "Suche abgebrochen oder Zeitlimit erreicht. Bisherige Treffer bleiben gespeichert."
	}
	encoded, _ := json.Marshal(job)
	s.scanMu.Unlock()
	_ = setSetting(s.db, "last_port_scan", string(encoded))
	event(s.db, "scan", "Interne TCP-Portsuche beendet")
}

func (s *Service) scanAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.scanControlMu.Lock()
		defer s.scanControlMu.Unlock()
	}
	if s.cfg.Role != "client" {
		fail(w, 409, "Suche nur auf dem Heim-CT")
		return
	}
	switch r.Method {
	case "GET":
		snapshot := s.scanSnapshot()
		snapshot["domain"] = s.discoveryDomain()
		snapshot["oracle_ip"] = s.cfg.PublicIP
		rows, err := s.db.Query("SELECT r.target_ip,r.target_port,r.protocol,COALESCE(s.name,''),COALESCE(s.backend,'') FROM scan_results r LEFT JOIN scan_services s ON s.target_ip=r.target_ip AND s.target_port=r.target_port ORDER BY r.target_ip,r.target_port LIMIT 4096")
		if err != nil {
			fail(w, 500, "Datenbank nicht verfügbar")
			return
		}
		out := []map[string]any{}
		for rows.Next() {
			var ip, proto, name, backend string
			var port int
			if rows.Scan(&ip, &port, &proto, &name, &backend) == nil {
				out = append(out, map[string]any{"target_ip": ip, "target_port": port, "protocol": proto, "name": name, "backend": backend})
			}
		}
		rows.Close()
		snapshot["results"] = out
		jsonOut(w, 200, snapshot)
	case "DELETE":
		s.scanMu.Lock()
		if s.scan != nil && s.scan.Running {
			s.scan.cancel()
		}
		s.scanMu.Unlock()
		jsonOut(w, 202, map[string]string{"message": "Abbruch angefordert. Bereits gespeicherte Treffer bleiben erhalten."})
	case "POST":
		var input struct {
			Targets string `json:"targets"`
			Mode    string `json:"mode"`
			Domain  string `json:"domain"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "Ungültige Suchanfrage")
			return
		}
		ips, ports, err := scanPlan(hostOnly(s.cfg.Listen), input.Targets, input.Mode)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		input.Domain = strings.ToLower(strings.TrimSpace(input.Domain))
		if input.Domain != "" && (!domainRE.MatchString(input.Domain) || len(input.Domain) > 253) {
			fail(w, 400, "Eine gültige öffentliche Domain ohne https:// eingeben")
			return
		}
		s.scanMu.Lock()
		if s.scan != nil && s.scan.Running {
			s.scanMu.Unlock()
			fail(w, 409, "Eine Suche läuft bereits")
			return
		}
		// Do not close the database until the preceding worker has fully finished.
		if s.scan != nil && s.scan.finished != nil {
			select {
			case <-s.scan.finished:
			default:
				s.scanMu.Unlock()
				fail(w, 409, "Vorherige Suche wird noch beendet")
				return
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		job := &scanJob{Running: true, Targets: strings.Join(ips, ","), Mode: input.Mode, Total: len(ips) * len(ports), Started: time.Now().UTC().Format(time.RFC3339), Message: "TCP-Ports werden geprüft. UDP erfordert eine passende Dienstprüfung oder manuelle Freigabe.", cancel: cancel, finished: make(chan struct{})}
		s.scan = job
		s.scanMu.Unlock()
		if input.Domain != "" {
			_ = setSetting(s.db, "discovery_domain", input.Domain)
		}
		encoded, _ := json.Marshal(job)
		_ = setSetting(s.db, "last_port_scan", string(encoded))
		go func() { defer cancel(); s.runPortScan(ctx, job, ips, ports) }()
		jsonOut(w, 202, s.scanSnapshot())
	default:
		fail(w, 405, "Methode nicht erlaubt")
	}
}
