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
		candidate := PortForward{Name: "Dienst " + strconv.Itoa(choice.TargetPort), Protocol: choice.Protocol, TargetIP: choice.TargetIP, TargetPort: choice.TargetPort, PublicPort: choice.TargetPort, Enabled: true}
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
	jsonOut(w, 202, map[string]any{"ports": ports, "message": "Auswahl gespeichert. Tunnel und Freigaben werden im Hintergrund eingerichtet."})
}
