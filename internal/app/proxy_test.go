package app

import (
	"strings"
	"testing"
)

func TestCaddyfile(t *testing.T) {
	v := caddyfile([]Route{{Domain: "home.example.com", Backend: "http://192.168.1.2", Enabled: true}, {Domain: "off.example.com", Backend: "http://192.168.1.3"}})
	if !strings.Contains(v, "home.example.com {") || !strings.Contains(v, "reverse_proxy 10.77.0.2:9080") || strings.Contains(v, "off.example.com") {
		t.Fatalf("unexpected config: %s", v)
	}
}
