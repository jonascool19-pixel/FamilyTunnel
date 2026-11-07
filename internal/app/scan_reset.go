package app

import "net/http"

func (s *Service) resetScanAPI(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Role != "client" || r.Method != "POST" {
		fail(w, 405, "POST auf dem Heim-CT erforderlich")
		return
	}
	s.scanControlMu.Lock()
	defer s.scanControlMu.Unlock()
	s.stopScan() // Wait until the canceled worker can no longer write old results.
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		fail(w, 500, "Suchdaten nicht verfügbar")
		return
	}
	defer tx.Rollback()
	for _, query := range []string{
		`DELETE FROM port_forwards WHERE enabled=0 AND EXISTS (SELECT 1 FROM scan_results r WHERE r.target_ip=port_forwards.target_ip AND r.target_port=port_forwards.target_port AND r.protocol=port_forwards.protocol)`,
		`DELETE FROM scan_results`, `DELETE FROM scan_services`, `DELETE FROM settings WHERE k='last_port_scan'`,
	} {
		if _, err = tx.Exec(query); err != nil {
			fail(w, 500, "Suchergebnisse konnten nicht zurückgesetzt werden")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "Zurücksetzen konnte nicht gespeichert werden")
		return
	}
	s.scanMu.Lock()
	s.scan = nil
	s.scanMu.Unlock()
	event(s.db, "scan", "Gespeicherte Suchtreffer und inaktive Suchvorschläge zurückgesetzt")
	jsonOut(w, 200, map[string]string{"message": "Suchergebnisse zurückgesetzt. Aktivierte Dienste bleiben erhalten. Du kannst eine neue Suche starten."})
}
