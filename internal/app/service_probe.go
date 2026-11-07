package app

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Probe only an already-discovered LAN endpoint. Redirects stay on that endpoint,
// are bounded to three hops, and HTTPS certificate verification stays enabled.
func probeLANService(ctx context.Context, ip string, port int) (string, string) {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 300 * time.Millisecond}).DialContext, TLSHandshakeTimeout: 600 * time.Millisecond, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 900 * time.Millisecond, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.Host != via[0].URL.Host || req.URL.User != nil {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	for _, scheme := range []string{"http", "https"} {
		backend := scheme + "://" + net.JoinHostPort(ip, strconv.Itoa(port))
		req, err := http.NewRequestWithContext(ctx, "GET", backend+"/", nil)
		if err != nil {
			continue
		}
		response, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		response.Body.Close()
		if scheme == "http" && response.StatusCode == 400 && strings.Contains(strings.ToLower(string(body)), "http request to an https server") {
			continue
		}
		name := "Webdienst"
		if match := titleRE.FindSubmatch(body); len(match) == 2 {
			title := strings.Join(strings.Fields(html.UnescapeString(string(match[1]))), " ")
			if title != "" && len(title) <= 80 {
				name = title
			}
		}
		return name, backend
	}
	return fmt.Sprintf("TCP-Dienst %d", port), ""
}

func (s *Service) discoveryDomain() string {
	if domain := setting(s.db, "discovery_domain"); domain != "" {
		return domain
	}
	rs, err := routes(s.db)
	if err != nil {
		return ""
	}
	domain := ""
	for _, r := range rs {
		if domain != "" && domain != r.Domain {
			return ""
		}
		domain = r.Domain
	}
	return domain
}
