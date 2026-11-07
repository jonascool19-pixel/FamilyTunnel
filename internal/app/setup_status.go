package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

type setupView struct {
	State     string `json:"state"`
	Message   string `json:"message"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Service) setSetup(state, message string) {
	s.setupMu.Lock()
	s.setup = setupView{state, message, time.Now().UTC().Format(time.RFC3339)}
	s.setupMu.Unlock()
}
func (s *Service) setupSnapshot() setupView {
	s.setupMu.RLock()
	defer s.setupMu.RUnlock()
	view := s.setup
	if view.State == "" {
		view.State = "waiting"
		view.Message = "Noch keine Einrichtungsbestätigung. Der Heim-Client prüft die Verbindung zu Oracle im Hintergrund."
	}
	return view
}
func (s *Service) configSnapshot() ([]Route, []PortForward, error) {
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	web, e := routes(s.db)
	if e != nil {
		return nil, nil, e
	}
	ports, e := portForwards(s.db)
	return web, ports, e
}
func configDigest(web []Route, ports []PortForward) string {
	b, _ := json.Marshal(struct {
		Web   []Route
		Ports []PortForward
	}{web, ports})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
