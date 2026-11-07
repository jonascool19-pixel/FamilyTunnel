package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSiteVerificationScopeAndValidation(t *testing.T) {
	dir := t.TempDir()
	if proofs, err := loadSiteVerifications(dir); err != nil || len(proofs) != 0 {
		t.Fatalf("optional config: %v", err)
	}
	path := filepath.Join(dir, "site-verification.json")
	valid := `[{"domain":"home.example.com","file":"google0123456789abcdef.html"}]`
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	proofs, err := loadSiteVerifications(dir)
	if err != nil {
		t.Fatal(err)
	}
	config := caddyfileVerified([]Route{{Domain: "home.example.com", PublicPort: 8096, Enabled: true}, {Domain: "other.example.com", Enabled: true}, {Domain: "disabled.example.com"}}, proofs)
	if strings.Count(config, "respond /google0123456789abcdef.html") != 1 || !strings.Contains(config, "https://home.example.com:8096") || strings.Contains(config, "disabled.example.com") || !strings.Contains(config, "reverse_proxy 10.77.0.2:9080") {
		t.Fatalf("unexpected scope: %s", config)
	}
	for _, bad := range []string{
		`[{"domain":"home.example.com","file":"../../secret"}]`,
		`[{"domain":"home.example.com\n}","file":"google0123456789abcdef.html"}]`,
		`[{"domain":"home.example.com","file":"google0123456789abcdef.html","body":"custom"}]`,
		valid + ` {}`,
		strings.Repeat(" ", 16385) + valid,
	} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSiteVerifications(dir); err == nil {
			t.Fatalf("accepted invalid proof: %q", bad)
		}
	}
}
