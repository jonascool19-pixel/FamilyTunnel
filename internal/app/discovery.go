package app

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type discoveredService struct {
	Name    string `json:"name"`
	Backend string `json:"backend"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
}

var titleRE = regexp.MustCompile(`(?is)<title[^>]*>\s*(.*?)\s*</title>`)

var commonWebPorts = []struct {
	port int
	name string
}{
	{80, "Webdienst"},
	{3000, "Grafana oder Webdienst"},
	{5000, "Webdienst"},
	{8000, "Webdienst"},
	{8080, "Webdienst"},
	{8096, "Jellyfin"},
	{8123, "Home Assistant"},
	{9000, "Portainer oder Webdienst"},
	{32400, "Plex"},
}

func scanIPv4s(admin string) ([]string, error) {
	ip := net.ParseIP(admin).To4()
	if ip == nil || !ip.IsPrivate() || ip.IsLoopback() {
		return nil, fmt.Errorf("keine private IPv4 für die Suche verfügbar")
	}
	// A /24 keeps discovery bounded to at most 254 hosts even on a larger LAN.
	result := make([]string, 0, 253)
	for n := 1; n < 255; n++ {
		candidate := net.IPv4(ip[0], ip[1], ip[2], byte(n)).String()
		if candidate != admin {
			result = append(result, candidate)
		}
	}
	return result, nil
}

func serviceTitle(ctx context.Context, ip string, port int, fallback string) string {
	client := &http.Client{
		Timeout: 1200 * time.Millisecond,
		Transport: &http.Transport{
			Proxy:       nil,
			DialContext: (&net.Dialer{Timeout: 350 * time.Millisecond}).DialContext,
		},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:%d/", ip, port), nil)
	resp, err := client.Do(req)
	if err != nil {
		return fallback
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	match := titleRE.FindSubmatch(body)
	if len(match) != 2 {
		return fallback
	}
	title := strings.Join(strings.Fields(html.UnescapeString(string(match[1]))), " ")
	if title == "" || len(title) > 80 {
		return fallback
	}
	return title
}

func discoverLAN(ctx context.Context, adminIP string) ([]discoveredService, error) {
	ips, err := scanIPv4s(adminIP)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	type target struct {
		ip   string
		port int
		name string
	}
	tasks := make(chan target)
	results := make(chan discoveredService, 64)
	var workers sync.WaitGroup
	for i := 0; i < 64; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for t := range tasks {
				conn, err := (&net.Dialer{Timeout: 300 * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort(t.ip, fmt.Sprint(t.port)))
				if err != nil {
					continue
				}
				_ = conn.Close()
				name := serviceTitle(ctx, t.ip, t.port, t.name)
				select {
				case results <- discoveredService{Name: name, Backend: fmt.Sprintf("http://%s:%d", t.ip, t.port), IP: t.ip, Port: t.port}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(tasks)
		for _, ip := range ips {
			for _, item := range commonWebPorts {
				select {
				case tasks <- target{ip: ip, port: item.port, name: item.name}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	services := []discoveredService{}
	for item := range results {
		services = append(services, item)
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].IP == services[j].IP {
			return services[i].Port < services[j].Port
		}
		return services[i].IP < services[j].IP
	})
	return services, nil
}

func (s *Service) discoveryAPI(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Role != "client" {
		fail(w, 409, "Die Dienstsuche läuft auf dem Heim-Client.")
		return
	}
	if r.Method != http.MethodPost {
		fail(w, 405, "POST required")
		return
	}
	var in struct {
		Domain string `json:"domain"`
	}
	if decode(w, r, &in) != nil {
		fail(w, 400, "Ungültige Anfrage.")
		return
	}
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	if !domainRE.MatchString(domain) {
		fail(w, 400, "Bitte einen gültigen Domainnamen ohne https:// eingeben.")
		return
	}
	if !s.discoveryMu.TryLock() {
		fail(w, 429, "Die Heimnetzsuche läuft bereits.")
		return
	}
	defer s.discoveryMu.Unlock()
	adminIP := hostOnly(s.cfg.Listen)
	services, err := discoverLAN(r.Context(), adminIP)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	resolved, dnsErr := net.DefaultResolver.LookupHost(r.Context(), domain)
	matches := false
	for _, ip := range resolved {
		if ip == s.cfg.PublicIP {
			matches = true
		}
	}
	jsonOut(w, 200, map[string]any{
		"domain":          domain,
		"oracle_ip":       s.cfg.PublicIP,
		"resolved_ips":    resolved,
		"dns_matches_vps": dnsErr == nil && matches,
		"services":        services,
	})
}
