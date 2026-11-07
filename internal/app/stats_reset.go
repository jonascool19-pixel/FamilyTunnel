package app

import "net/http"

func (s *Service) resetStatsAPI(w http.ResponseWriter, r *http.Request) {
	// The same lock guards flushing: old buffered counts cannot reappear later.
	s.traffic.mu.Lock()
	_, err := s.db.Exec("DELETE FROM stats")
	if err == nil {
		s.traffic.data = map[string]counters{}
	}
	s.traffic.mu.Unlock()
	if err != nil {
		fail(w, 500, "Statistik konnte nicht zurückgesetzt werden")
		return
	}
	event(s.db, "statistics", "Statistik dieses Controllers zurückgesetzt")
	jsonOut(w, 200, map[string]string{"message": "Statistik zurückgesetzt. Neue Zugriffe werden ab jetzt wieder gezählt."})
}
