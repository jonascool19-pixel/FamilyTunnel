package app

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

func dialLAN(ctx context.Context, ip string, port int) error {
	c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err == nil {
		c.Close()
	}
	return err
}

// Recheck the entire selection before changing any routes or firewall rules.
// A stored scan result is not evidence that an endpoint is still listening.
func (s *Service) preflightChoices(ctx context.Context, choices []publishChoice) error {
	for _, p := range choices {
		candidate := PortForward{Protocol: strings.ToLower(p.Protocol), TargetIP: p.TargetIP, TargetPort: p.TargetPort, PublicPort: 1024}
		if err := candidate.validate(); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	check := s.checkLAN
	if check == nil {
		check = dialLAN
	}
	results := make([]error, len(choices))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 16)
	for i, p := range choices {
		if strings.ToLower(p.Protocol) != "tcp" {
			continue // UDP needs a service-specific reply; a dial cannot prove it.
		}
		wg.Add(1)
		go func(i int, p publishChoice) {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				results[i] = ctx.Err()
				return
			}
			results[i] = check(ctx, p.TargetIP, p.TargetPort)
		}(i, p)
	}
	wg.Wait()
	for i, err := range results {
		if err != nil {
			p := choices[i]
			return fmt.Errorf("Vom Heim-CT nicht erreichbar: %s. Keine Freigaben dieser Auswahl eingerichtet. Dienst starten oder erneut suchen", net.JoinHostPort(p.TargetIP, strconv.Itoa(p.TargetPort)))
		}
	}
	return nil
}

// A new scan replaces cached discoveries for the selected devices. Active
// services and unrelated/manual drafts are retained even if a device is down.
func (s *Service) clearScannedTargets(ips []string) error {
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, ip := range ips {
		for _, query := range []string{
			`DELETE FROM port_forwards WHERE enabled=0 AND target_ip=? AND EXISTS (SELECT 1 FROM scan_results r WHERE r.target_ip=port_forwards.target_ip AND r.target_port=port_forwards.target_port AND r.protocol=port_forwards.protocol)`,
			`DELETE FROM scan_services WHERE target_ip=?`,
			`DELETE FROM scan_results WHERE target_ip=?`,
		} {
			if _, err = tx.Exec(query, ip); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
