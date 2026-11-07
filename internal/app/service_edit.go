package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

func editBothPorts(old []PortForward, p PortForward, id int64) ([]PortForward, error) {
	next := int64(1)
	var original *PortForward
	for _, v := range old {
		if v.ID >= next {
			next = v.ID + 1
		}
		if v.ID == id {
			copy := v
			original = &copy
		}
	}
	if id != 0 && original == nil {
		return nil, fmt.Errorf("Freigabe nicht gefunden")
	}
	result := make([]PortForward, 0, len(old)+2)
	ids := map[string]int64{}
	for _, v := range old {
		matched := v.ID == id || (original != nil && v.PublicPort == original.PublicPort && v.TargetIP == original.TargetIP && v.TargetPort == original.TargetPort)
		if matched {
			ids[v.Protocol] = v.ID
			continue
		}
		if v.PublicPort == p.PublicPort {
			return nil, fmt.Errorf("TCP oder UDP auf diesem öffentlichen Port bereits vergeben")
		}
		result = append(result, v)
	}
	for _, proto := range []string{"tcp", "udp"} {
		copy := p
		copy.Protocol = proto
		copy.ID = ids[proto]
		if copy.ID == 0 {
			copy.ID = next
			next++
		}
		if err := copy.validate(); err != nil {
			return nil, err
		}
		result = append(result, copy)
	}
	return result, nil
}

// Save the HTTPS route and optional UDP companion in one DB transaction.
func (s *Service) webServiceEditAPI(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Role != "client" || r.Method != "POST" {
		fail(w, 405, "POST auf dem Heim-CT erforderlich")
		return
	}
	var in struct {
		Route Route `json:"route"`
		UDP   bool  `json:"udp"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if err := in.Route.validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	web, err := routes(s.db)
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	ports, err := portForwards(s.db)
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	next := int64(1)
	found := false
	var old *Route
	for i, v := range web {
		if v.ID >= next {
			next = v.ID + 1
		}
		if in.Route.ID != 0 && v.ID == in.Route.ID {
			copy := v
			old = &copy
			web[i] = in.Route
			found = true
		}
	}
	if in.Route.ID == 0 {
		in.Route.ID = next
		web = append(web, in.Route)
	} else if !found {
		fail(w, 404, "Webdienst nicht gefunden")
		return
	}
	companionID := int64(0)
	filtered := make([]PortForward, 0, len(ports)+1)
	nextPort := int64(1)
	for _, p := range ports {
		if p.ID >= nextPort {
			nextPort = p.ID + 1
		}
		if old != nil && p.Protocol == "udp" && p.PublicPort == old.port() && backendMatchesChoice(old.Backend, publishChoice{TargetIP: p.TargetIP, TargetPort: p.TargetPort}) {
			companionID = p.ID
			continue
		}
		filtered = append(filtered, p)
	}
	if in.UDP {
		u, _ := url.Parse(in.Route.Backend)
		port := u.Port()
		if port == "" {
			port = "80"
			if u.Scheme == "https" {
				port = "443"
			}
		}
		target, _ := strconv.Atoi(port)
		if companionID == 0 {
			companionID = nextPort
		}
		filtered = append(filtered, PortForward{ID: companionID, Name: in.Route.Name, Protocol: "udp", TargetIP: u.Hostname(), TargetPort: target, PublicPort: in.Route.port(), Enabled: in.Route.Enabled})
	}
	if err = replaceAllRoutes(s.db, web, filtered); err != nil {
		fail(w, 409, err.Error())
		return
	}
	s.notifyRoutes()
	event(s.db, "services", "Webdienst und Protokollauswahl gemeinsam gespeichert")
	jsonOut(w, 202, map[string]any{"routes": web, "ports": filtered, "message": "Dienst und Protokolle gespeichert. Einrichtung läuft im Hintergrund."})
}
