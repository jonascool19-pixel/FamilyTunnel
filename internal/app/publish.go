package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type publishChoice struct {
	TargetIP   string `json:"target_ip"`
	TargetPort int    `json:"target_port"`
	Protocol   string `json:"protocol"`
	Auto       bool   `json:"auto"`
	Domain     string `json:"domain"`
}

func backendMatchesChoice(raw string, choice publishChoice) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != choice.TargetIP {
		return false
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return port == strconv.Itoa(choice.TargetPort)
}

// Plan both web and direct services in memory; the complete selection commits once.
func (s *Service) planAutoWeb(choice publishChoice, web *[]Route, ports *[]PortForward) (bool, error) {
	if !choice.Auto || choice.Protocol != "tcp" {
		return false, nil
	}
	for _, p := range *ports {
		if p.Enabled && p.Protocol == "tcp" && p.TargetIP == choice.TargetIP && p.TargetPort == choice.TargetPort {
			return true, nil
		}
	}
	var backend string
	_ = s.db.QueryRow("SELECT backend FROM scan_services WHERE target_ip=? AND target_port=?", choice.TargetIP, choice.TargetPort).Scan(&backend)
	if backend == "" {
		for _, r := range *web {
			u, e := url.Parse(r.Backend)
			if e == nil && u.Hostname() == choice.TargetIP {
				port := u.Port()
				if port == "" {
					port = "80"
					if u.Scheme == "https" {
						port = "443"
					}
				}
				if port == strconv.Itoa(choice.TargetPort) {
					backend = r.Backend
					break
				}
			}
		}
	}
	if backend == "" {
		return false, nil
	}
	for _, r := range *web {
		if backendMatchesChoice(r.Backend, choice) && r.Enabled {
			return true, nil
		}
	}
	if choice.TargetPort == 22 || choice.TargetPort == 2019 || choice.TargetPort == 8443 || choice.TargetPort == 9080 || choice.TargetPort == 9081 {
		return true, fmt.Errorf("Verwaltungsports werden nicht automatisch veröffentlicht")
	}
	domain := strings.ToLower(strings.TrimSpace(choice.Domain))
	if domain == "" {
		domain = s.discoveryDomain()
	}
	if domain == "" {
		for _, r := range *web {
			if backendMatchesChoice(r.Backend, choice) {
				domain = r.Domain
				break
			}
		}
	}
	candidate := Route{Domain: domain, Backend: backend, Enabled: true, PublicPort: choice.TargetPort}
	_ = s.db.QueryRow("SELECT name FROM scan_services WHERE target_ip=? AND target_port=?", choice.TargetIP, choice.TargetPort).Scan(&candidate.Name)
	if candidate.PublicPort < 1024 {
		candidate.PublicPort = 443
	}
	if err := candidate.validate(); err != nil {
		return true, fmt.Errorf("Für Webdienste oben einmal eine gültige öffentliche Domain eintragen: %w", err)
	}
	index := -1
	next := int64(1)
	for i, r := range *web {
		if r.ID >= next {
			next = r.ID + 1
		}
		if backendMatchesChoice(r.Backend, choice) {
			if r.Enabled {
				return true, nil
			}
			index = i
			candidate = r
			candidate.Enabled = true
		}
	}
	filtered := make([]PortForward, 0, len(*ports))
	for _, p := range *ports {
		if !p.Enabled && p.Protocol == "tcp" && p.TargetIP == choice.TargetIP && p.TargetPort == choice.TargetPort {
			continue
		}
		filtered = append(filtered, p)
	}
	used := map[int]bool{}
	for _, p := range filtered {
		if p.Protocol == "tcp" {
			used[p.PublicPort] = true
		}
	}
	for i, r := range *web {
		if r.Enabled && i != index {
			used[r.port()] = true
		}
	}
	for !candidate.validPort() || used[candidate.port()] {
		candidate.PublicPort = candidate.port() + 1
		if candidate.PublicPort > 65535 {
			return true, fmt.Errorf("Kein freier öffentlicher HTTPS-Port verfügbar")
		}
	}
	if index < 0 {
		candidate.ID = next
		*web = append(*web, candidate)
	} else {
		(*web)[index] = candidate
	}
	*ports = filtered
	return true, nil
}

func (s *Service) publishPortsAPI(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Role != "client" || r.Method != "POST" {
		fail(w, 405, "POST auf dem Heim-CT erforderlich")
		return
	}
	var choices []publishChoice
	if decode(w, r, &choices) != nil || len(choices) == 0 || len(choices) > 200 {
		fail(w, 400, "Zwischen 1 und 200 Ports auswählen")
		return
	}
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	ports, err := portForwards(s.db)
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	web, err := routes(s.db)
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	used := map[string]bool{}
	next := int64(1)
	for _, p := range ports {
		used[p.Protocol+":"+strconv.Itoa(p.PublicPort)] = true
		if p.ID >= next {
			next = p.ID + 1
		}
	}
	for _, route := range web {
		if route.Enabled {
			used["tcp:"+strconv.Itoa(route.port())] = true
		}
	}
	for _, choice := range choices {
		choice.Protocol = strings.ToLower(choice.Protocol)
		handled, e := s.planAutoWeb(choice, &web, &ports)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		if handled {
			continue
		}
		used = map[string]bool{}
		for _, p := range ports {
			used[p.Protocol+":"+strconv.Itoa(p.PublicPort)] = true
		}
		for _, r := range web {
			if r.Enabled {
				used["tcp:"+strconv.Itoa(r.port())] = true
			}
		}
		candidate := PortForward{Name: "Dienst " + strconv.Itoa(choice.TargetPort), Protocol: choice.Protocol, TargetIP: choice.TargetIP, TargetPort: choice.TargetPort, PublicPort: choice.TargetPort, Enabled: true}
		if choice.Auto {
			var name string
			_ = s.db.QueryRow("SELECT name FROM scan_services WHERE target_ip=? AND target_port=?", choice.TargetIP, choice.TargetPort).Scan(&name)
			if name != "" {
				candidate.Name = name
			}
		}
		if err = candidate.validate(); err != nil {
			fail(w, 400, err.Error())
			return
		}
		// Preserve an existing HTTPS site on this same public port rather than replacing it with plain TCP.
		existingWeb := false
		if choice.Protocol == "tcp" {
			for _, route := range web {
				u, _ := url.Parse(route.Backend)
				if u == nil {
					continue
				}
				port := u.Port()
				if port == "" {
					port = "80"
					if u.Scheme == "https" {
						port = "443"
					}
				}
				if route.Enabled && route.port() == choice.TargetPort && u.Hostname() == choice.TargetIP && port == strconv.Itoa(choice.TargetPort) {
					existingWeb = true
					break
				}
			}
		}
		if existingWeb {
			continue
		}
		index := -1
		for i, p := range ports {
			if p.Protocol == choice.Protocol && p.TargetIP == choice.TargetIP && p.TargetPort == choice.TargetPort {
				index = i
				break
			}
		}
		if index >= 0 && ports[index].Enabled {
			continue
		}
		if index >= 0 {
			candidate = ports[index]
			candidate.Enabled = true
			claimedByWeb := false
			for _, route := range web {
				if route.Enabled && candidate.Protocol == "tcp" && route.port() == candidate.PublicPort {
					claimedByWeb = true
				}
			}
			if !claimedByWeb {
				delete(used, candidate.Protocol+":"+strconv.Itoa(candidate.PublicPort))
			}
		}
		for candidate.validate() != nil || used[candidate.Protocol+":"+strconv.Itoa(candidate.PublicPort)] {
			candidate.PublicPort++
			if candidate.PublicPort > 65535 {
				fail(w, 409, "Kein freier öffentlicher Port verfügbar")
				return
			}
		}
		if err = candidate.validate(); err != nil {
			fail(w, 409, "Für diesen Dienst einen anderen öffentlichen Port unter Erweitert wählen")
			return
		}
		used[candidate.Protocol+":"+strconv.Itoa(candidate.PublicPort)] = true
		if index >= 0 {
			ports[index] = candidate
		} else {
			candidate.ID = next
			next++
			ports = append(ports, candidate)
		}
	}
	if err = replaceAllRoutes(s.db, web, ports); err != nil {
		fail(w, 409, err.Error())
		return
	}
	event(s.db, "publish", fmt.Sprintf("Freigabe für %d ausgewählte Ports angefordert", len(choices)))
	s.notifyRoutes()
	for _, choice := range choices {
		domain := strings.ToLower(strings.TrimSpace(choice.Domain))
		if domain != "" && domainRE.MatchString(domain) && len(domain) <= 253 {
			_ = setSetting(s.db, "discovery_domain", domain)
			break
		}
	}
	jsonOut(w, 202, map[string]any{"ports": ports, "routes": web, "message": "Auswahl gespeichert. HTTPS, Tunnel und Oracle-Freigaben werden im Hintergrund eingerichtet."})
}
