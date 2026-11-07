package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type cloudTarget struct {
	Enabled bool   `json:"enabled"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
}

func (s *Service) applyCloud(ctx context.Context) error {
	file := filepath.Join(s.cfg.Dir, "oci-target.json")
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var target cloudTarget
	if json.Unmarshal(b, &target) != nil {
		return fmt.Errorf("Ungültige Oracle-Anbindung")
	}
	if !target.Enabled {
		return nil
	}
	output, err := exec.CommandContext(ctx, "python3", "/usr/local/lib/familytunnel/oci-nsg.py", "--target-file", file, "--state-dir", s.cfg.Dir).CombinedOutput()
	if err != nil {
		event(s.db, "cloud", "Oracle-Freigabe fehlgeschlagen; OCI CLI, IAM-Rechte und Ziel prüfen")
		return fmt.Errorf("Oracle-Automation fehlgeschlagen: OCI CLI, Anmeldung und IAM-Rechte für die gewählte Sicherheitsliste/NSG prüfen")
	}
	_ = output
	event(s.db, "cloud", "Oracle-Portregeln synchronisiert")
	return nil
}

func (s *Service) cloudAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		fail(w, 405, "GET oder POST erforderlich")
		return
	}
	var target cloudTarget
	if r.Method == "POST" {
		if decode(w, r, &target) != nil {
			fail(w, 400, "Ungültige Oracle-Anbindung")
			return
		}
		prefix := "ocid1.networksecuritygroup."
		if target.Kind == "security-list" {
			prefix = "ocid1.securitylist."
		} else if target.Kind != "nsg" {
			fail(w, 400, "NSG oder Sicherheitsliste auswählen")
			return
		}
		if target.Enabled && (!strings.HasPrefix(target.ID, prefix) || len(target.ID) > 512 || strings.ContainsAny(target.ID, " \r\n\t")) {
			fail(w, 400, "Die passende Oracle-OCID eingeben")
			return
		}
	}
	if s.cfg.Role == "client" {
		var body io.Reader
		if r.Method == "POST" {
			b, _ := json.Marshal(target)
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, s.cfg.ServerURL+"/internal/cloud", body)
		if err != nil {
			fail(w, 500, "Ungültige Serverkonfiguration")
			return
		}
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
		req.Header.Set("Content-Type", "application/json")
		response, err := controlHTTP.Do(req)
		if err != nil {
			fail(w, 503, "Oracle über den Tunnel nicht erreichbar")
			return
		}
		defer response.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(response.Body, 65536))
		return
	}
	file := filepath.Join(s.cfg.Dir, "oci-target.json")
	if r.Method == "GET" {
		b, err := os.ReadFile(file)
		if err == nil {
			_ = json.Unmarshal(b, &target)
		}
		jsonOut(w, 200, target)
		return
	}
	b, _ := json.Marshal(target)
	if err := writeAtomic(file, b, 0600); err != nil {
		fail(w, 500, "Oracle-Anbindung konnte nicht gespeichert werden")
		return
	}
	// No arbitrary shell commands or API credentials are accepted through the web UI.
	jsonOut(w, 200, map[string]string{"message": "Oracle-Anbindung gespeichert. Freigaben werden bei der nächsten Konfigurationsübertragung synchronisiert. OCI CLI und IAM-Rechte müssen auf Oracle eingerichtet sein."})
}
