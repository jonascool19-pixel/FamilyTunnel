package app

import "net/http"

func (s *Service) logsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		result, err := s.db.Exec("DELETE FROM events")
		if err != nil {
			fail(w, 500, "Ereignisse konnten nicht gelöscht werden")
			return
		}
		count, _ := result.RowsAffected()
		jsonOut(w, 200, map[string]any{"deleted": count, "message": "Alle bisherigen Ereignisse wurden gelöscht. Neue Ereignisse werden weiterhin aufgezeichnet."})
		return
	}
	if r.Method != "GET" {
		fail(w, 405, "GET oder DELETE erforderlich")
		return
	}
	rows, err := s.db.Query("SELECT at,kind,message FROM events ORDER BY id DESC LIMIT 100")
	if err != nil {
		fail(w, 500, "Datenbank nicht verfügbar")
		return
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var at, kind, message string
		if rows.Scan(&at, &kind, &message) == nil {
			out = append(out, map[string]string{"at": at, "kind": kind, "message": message})
		}
	}
	jsonOut(w, 200, out)
}
