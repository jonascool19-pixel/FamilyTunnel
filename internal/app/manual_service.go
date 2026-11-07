package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (s *Service) manualServiceAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || s.cfg.Role != "client" {
		fail(w, 405, "POST auf dem Heim-CT erforderlich")
		return
	}
	var input struct {
		IP       string `json:"target_ip"`
		Ports    string `json:"ports"`
		Name     string `json:"name"`
		Domain   string `json:"domain"`
		Protocol string `json:"protocol"`
	}
	if decode(w, r, &input) != nil {
		fail(w, 400, "Ungültige Eingabe")
		return
	}
	input.IP = strings.TrimSpace(input.IP)
	input.Name = strings.TrimSpace(input.Name)
	if input.Protocol == "" {
		input.Protocol = "auto"
	}
	if input.Protocol != "auto" && input.Protocol != "tcp" && input.Protocol != "udp" && input.Protocol != "both" {
		fail(w, 400, "Automatisch, TCP oder UDP auswählen")
		return
	}
	numbers := []int{}
	seen := map[int]bool{}
	for _, part := range strings.FieldsFunc(input.Ports, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		port, err := strconv.Atoi(part)
		if err != nil || port < 1 || port > 65535 {
			fail(w, 400, "Gültige Ports von 1 bis 65535 eingeben")
			return
		}
		if !seen[port] {
			numbers = append(numbers, port)
			seen[port] = true
		}
	}
	if len(numbers) == 0 || len(numbers) > 32 {
		fail(w, 400, "Zwischen 1 und 32 Ports eingeben")
		return
	}
	check := PortForward{Name: input.Name, Protocol: "tcp", TargetIP: input.IP, TargetPort: numbers[0], PublicPort: 1024}
	if err := check.validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	type found struct{ name, backend, pin string }
	results := make([]found, len(numbers))
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	limit := make(chan struct{}, 8)
	for i, port := range numbers {
		workers.Add(1)
		go func(i, port int) {
			defer workers.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-limit }()
			name, backend, pin := "Dienst "+strconv.Itoa(port), "", ""
			if input.Protocol == "auto" {
				if s.probeService != nil {
					name, backend, pin = s.probeService(ctx, input.IP, port)
				} else {
					name, backend, pin = probeLANServiceInfo(ctx, input.IP, port, true)
				}
			}
			if input.Name != "" {
				name = input.Name
			}
			results[i] = found{name, backend, pin}
		}(i, port)
	}
	workers.Wait()
	if ctx.Err() != nil {
		fail(w, 503, "Erkennung dauert zu lange. Weniger Ports gleichzeitig versuchen.")
		return
	}
	choices := []publishChoice{}
	for i, port := range numbers {
		info := results[i]
		if _, err := s.db.Exec("INSERT INTO scan_services(target_ip,target_port,name,backend,backend_cert_sha256) VALUES(?,?,?,?,?) ON CONFLICT(target_ip,target_port) DO UPDATE SET name=excluded.name,backend=excluded.backend,backend_cert_sha256=excluded.backend_cert_sha256", input.IP, port, info.name, info.backend, info.pin); err != nil {
			fail(w, 500, "Erkennung konnte nicht gespeichert werden")
			return
		}
		protocols := []string{input.Protocol}
		if input.Protocol == "auto" {
			protocols = []string{"tcp"}
		} else if input.Protocol == "both" {
			protocols = []string{"tcp", "udp"}
		}
		for _, protocol := range protocols {
			choices = append(choices, publishChoice{TargetIP: input.IP, TargetPort: port, Protocol: protocol, Auto: input.Protocol == "auto", Domain: input.Domain, Name: input.Name, ReplaceDirect: input.Protocol == "auto"})
		}
	}
	body, _ := json.Marshal(choices)
	request := r.Clone(r.Context())
	request.Body = http.NoBody
	if len(body) > 0 {
		request.Body = io.NopCloser(bytes.NewReader(body))
	}
	s.publishPortsAPI(w, request)
}
