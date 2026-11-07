package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type maintenancePlan struct {
	Enabled    bool      `json:"enabled"`
	Time       string    `json:"time"`
	Zone       string    `json:"zone"`
	Mode       string    `json:"mode"`
	Generation string    `json:"generation"`
	Next       time.Time `json:"next,omitempty"`
}
type maintenanceJob struct {
	ID           string    `json:"id"`
	Grant        string    `json:"grant"`
	Source       string    `json:"source"`
	State        string    `json:"state"`
	Message      string    `json:"message"`
	Created      time.Time `json:"created"`
	Expires      time.Time `json:"expires"`
	ServerBefore string    `json:"server_before"`
	ServerAfter  string    `json:"server_after,omitempty"`
	ClientBefore string    `json:"client_before"`
	StableSince  time.Time `json:"stable_since,omitempty"`
	ReadyAt      time.Time `json:"ready_at,omitempty"`
}
type maintenanceState struct {
	Plan        maintenancePlan `json:"plan"`
	Job         *maintenanceJob `json:"job,omitempty"`
	LastSlot    string          `json:"last_slot,omitempty"`
	Seen        time.Time       `json:"client_seen"`
	ClientBoot  string          `json:"client_boot"`
	ClientGrant string          `json:"client_grant"`
	TestGrant   string          `json:"test_grant"`
	ClientReady bool            `json:"client_ready"`
}
type maintenanceLocal struct {
	Plan     maintenancePlan `json:"plan"`
	Test     string          `json:"test"`
	Consumed string          `json:"consumed"`
	Error    string          `json:"error"`
}
type maintenanceInput struct {
	Action       string          `json:"action"`
	Plan         maintenancePlan `json:"plan"`
	Confirmation string          `json:"confirmation"`
	Boot         string          `json:"boot"`
	Grant        string          `json:"grant"`
	Test         string          `json:"test"`
	ID           string          `json:"id"`
	Ready        bool            `json:"ready"`
}

// Inject only in tests. Production reboot commands have fixed arguments and run
// from a separate root systemd worker, never from the HTTP controller.
type maintenanceRuntime struct {
	Now    func() time.Time
	Boot   func() string
	Ready  func(context.Context) bool
	Uptime func() time.Duration
	Reboot func(context.Context) error
}

const maintenanceStableFor = time.Minute

func (s *Service) maintenanceRuntime() maintenanceRuntime {
	if s.maintenance != nil {
		return *s.maintenance
	}
	return maintenanceRuntime{
		Now:  time.Now,
		Boot: maintenanceBootIdentity,
		Uptime: func() time.Duration {
			b, _ := os.ReadFile("/proc/uptime")
			fields := strings.Fields(string(b))
			if len(fields) == 0 {
				return 0
			}
			n, _ := strconv.ParseFloat(fields[0], 64)
			return time.Duration(n * float64(time.Second))
		},
		Ready: func(ctx context.Context) bool {
			services := []string{"is-active", "--quiet", "familytunnel"}
			if s.cfg.Role == "server" {
				services = append(services, "caddy")
			}
			for _, unit := range services[2:] {
				if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit).Run() != nil {
					return false
				}
			}
			b, err := exec.CommandContext(ctx, "wg", "show", "ft0", "latest-handshakes").Output()
			if err != nil {
				return false
			}
			fields := strings.Fields(string(b))
			for i := 1; i < len(fields); i += 2 {
				n, _ := strconv.ParseInt(fields[i], 10, 64)
				if n > 0 && time.Now().Unix()-n >= 0 && time.Now().Unix()-n < 180 {
					return true
				}
			}
			return false
		},
		Reboot: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "systemctl", "--no-block", "reboot").Run()
		},
	}
}

// PID 1 start time changes on an LXC reboot even with an unchanged host boot ID.
// A restart of the FamilyTunnel controller does not change this identity.
func maintenanceBootIdentity() string {
	b, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return ""
	}
	p, e := os.ReadFile("/proc/1/stat")
	if e != nil {
		return ""
	}
	end := strings.LastIndex(string(p), ")")
	if end < 0 {
		return ""
	}
	parts := strings.Fields(string(p)[end+1:])
	if len(parts) <= 19 {
		return ""
	}
	h := sha256.Sum256([]byte(strings.TrimSpace(string(b)) + ":" + parts[19]))
	v := h[:16]
	return fmt.Sprintf("%x-%x-%x-%x-%x", v[:4], v[4:6], v[6:8], v[8:10], v[10:])
}
func defaultMaintenancePlan() maintenancePlan {
	return maintenancePlan{Time: "07:00", Zone: "Europe/Berlin", Mode: "daily"}
}
func maintenanceActive(j *maintenanceJob) bool {
	return j != nil && j.State != "completed" && j.State != "failed"
}

var maintenanceID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)
var maintenanceBoot = regexp.MustCompile(`^[a-f0-9-]{36}$`)

func (p maintenancePlan) validate() error {
	if _, e := time.Parse("15:04", p.Time); e != nil {
		return errors.New("Uhrzeit muss HH:MM sein")
	}
	if p.Mode != "daily" && p.Mode != "interval24" {
		return errors.New("Ungültige Wiederholung")
	}
	if len(p.Zone) > 80 {
		return errors.New("Ungültige Zeitzone")
	}
	if _, e := time.LoadLocation(p.Zone); e != nil {
		return errors.New("Unbekannte Zeitzone")
	}
	if !maintenanceID.MatchString(p.Generation) {
		return errors.New("Ungültige Zeitplan-Kennung")
	}
	return nil
}
func nextMaintenanceTime(p maintenancePlan, now time.Time) time.Time {
	loc, _ := time.LoadLocation(p.Zone)
	local := now.In(loc)
	clock, _ := time.Parse("15:04", p.Time)
	// time.Date may normalize a nonexistent spring-time hour to another hour.
	// Skip that local date instead of rebooting at an unselected time.
	for day := 0; day < 8; day++ {
		next := time.Date(local.Year(), local.Month(), local.Day()+day, clock.Hour(), clock.Minute(), 0, 0, loc)
		if next.After(now) && next.In(loc).Format("15:04") == clock.Format("15:04") {
			return next.UTC()
		}
	}
	return now.AddDate(0, 0, 8).UTC()
}

// Compare-and-swap also serializes the HTTP process with the separate worker.
// No reboot/network side effects are allowed inside the retryable mutation.
func (s *Service) mutateMaintenance(key string, fn func(*maintenanceState) error) error {
	for attempt := 0; attempt < 12; attempt++ {
		if _, err := s.db.Exec("INSERT OR IGNORE INTO settings(k,v) VALUES(?,?)", key, `{}`); err != nil {
			return err
		}
		var old string
		if err := s.db.QueryRow("SELECT v FROM settings WHERE k=?", key).Scan(&old); err != nil {
			return err
		}
		state := maintenanceState{Plan: defaultMaintenancePlan()}
		if err := json.Unmarshal([]byte(old), &state); err != nil {
			return err
		}
		if err := fn(&state); err != nil {
			return err
		}
		b, err := json.Marshal(state)
		if err != nil {
			return err
		}
		result, err := s.db.Exec("UPDATE settings SET v=? WHERE k=? AND v=?", string(b), key, old)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n == 1 {
			return nil
		}
	}
	return errors.New("Wartung ist beschäftigt, bitte erneut versuchen")
}
func (s *Service) maintenanceState() (maintenanceState, error) {
	state := maintenanceState{Plan: defaultMaintenancePlan()}
	v := setting(s.db, "maintenance_state")
	if v == "" {
		return state, nil
	}
	err := json.Unmarshal([]byte(v), &state)
	return state, err
}
func (s *Service) maintenanceLocal() (maintenanceLocal, error) {
	l := maintenanceLocal{Plan: defaultMaintenancePlan()}
	v := setting(s.db, "maintenance_local")
	if v == "" {
		return l, nil
	}
	err := json.Unmarshal([]byte(v), &l)
	return l, err
}
func (s *Service) saveMaintenanceLocal(l maintenanceLocal) error {
	b, e := json.Marshal(l)
	if e != nil {
		return e
	}
	return setSetting(s.db, "maintenance_local", string(b))
}
func (s *Service) maintenanceRemote(ctx context.Context, in *maintenanceInput) (maintenanceState, error) {
	method := "GET"
	var body io.Reader
	if in != nil {
		method = "POST"
		b, e := json.Marshal(in)
		if e != nil {
			return maintenanceState{}, e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, s.cfg.ServerURL+"/internal/maintenance", body)
	if e != nil {
		return maintenanceState{}, e
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := controlHTTP.Do(req)
	if e != nil {
		return maintenanceState{}, errors.New("Oracle über WireGuard nicht erreichbar")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return maintenanceState{}, fmt.Errorf("Oracle-Wartung nicht verfügbar (HTTP %d). Beide Seiten aktualisieren", resp.StatusCode)
	}
	var out maintenanceState
	e = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&out)
	return out, e
}
func (s *Service) maintenanceInternalAPI(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Role != "server" {
		fail(w, 404, "Oracle endpoint only")
		return
	}
	if r.Method == "GET" {
		v, e := s.maintenanceState()
		if e != nil {
			fail(w, 500, "Wartungsdaten nicht lesbar")
			return
		}
		jsonOut(w, 200, v)
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "GET or POST required")
		return
	}
	var in maintenanceInput
	if decode(w, r, &in) != nil {
		return
	}
	now := s.maintenanceRuntime().Now().UTC()
	ackReady := false
	if in.Action == "ack" {
		rt := s.maintenanceRuntime()
		ackReady = rt.Ready(r.Context()) && rt.Uptime() >= maintenanceStableFor
	}
	e := s.mutateMaintenance("maintenance_state", func(v *maintenanceState) error {
		switch in.Action {
		case "settings":
			if e := in.Plan.validate(); e != nil {
				return e
			}
			if maintenanceActive(v.Job) {
				return errors.New("Erst den laufenden Neustart abschließen lassen")
			}
			v.Plan = in.Plan
			v.Plan.Next = nextMaintenanceTime(in.Plan, now)
		case "test":
			if in.Confirmation != "RESTART_BOTH" || !maintenanceID.MatchString(in.Test) || !maintenanceBoot.MatchString(in.Boot) {
				return errors.New("Bestätigung für beide Neustarts fehlt")
			}
			if maintenanceActive(v.Job) {
				return errors.New("Ein Neustart läuft bereits")
			}
			if v.Job != nil && v.Job.ID == in.Test {
				return errors.New("Dieser Test wurde bereits angefordert")
			}
			v.Job = &maintenanceJob{ID: in.Test, Grant: in.Test, Source: "test", State: "requested", Message: "Echter Test angefordert: zuerst Oracle, danach der FamilyTunnel-CT.", Created: now, Expires: now.Add(20 * time.Minute), ClientBefore: in.Boot}
		case "heartbeat":
			if !maintenanceBoot.MatchString(in.Boot) {
				return errors.New("Ungültige Boot-Kennung")
			}
			v.Seen = now
			v.ClientBoot = in.Boot
			v.ClientGrant = in.Grant
			v.TestGrant = in.Test
			v.ClientReady = in.Ready
			if v.Job != nil && v.Job.State == "client_rebooting" && in.Ready && in.Boot != v.Job.ClientBefore {
				v.Job.State = "completed"
				v.Job.Message = "Oracle und FamilyTunnel-CT wurden nacheinander neu gestartet."
			}
		case "ack":
			if v.Job == nil || v.Job.ID != in.ID || v.Job.State != "waiting_client" || !now.Before(v.Job.Expires) || in.Boot != v.Job.ClientBefore || !ackReady || s.maintenanceRuntime().Boot() != v.Job.ServerAfter || now.Sub(v.Job.ReadyAt) < 0 || now.Sub(v.Job.ReadyAt) > 45*time.Second {
				return errors.New("Neustartauftrag nicht freigegeben")
			}
			v.Job.State = "client_rebooting"
			v.Job.Message = "CT hat den Auftrag dauerhaft bestätigt und startet jetzt neu."
		case "fail":
			if v.Job == nil || v.Job.ID != in.ID {
				return errors.New("Unbekannter Auftrag")
			}
			v.Job.State = "failed"
			v.Job.Message = "CT-Neustart fehlgeschlagen. Kein automatischer Wiederholungsversuch."
		default:
			return errors.New("Unbekannte Wartungsaktion")
		}
		return nil
	})
	if e != nil {
		fail(w, 409, e.Error())
		return
	}
	v, e := s.maintenanceState()
	if e != nil {
		fail(w, 500, "Wartungsdaten nicht lesbar")
		return
	}
	jsonOut(w, 200, v)
}
func (s *Service) maintenanceAPI(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if r.Method == "GET" {
		var v maintenanceState
		var e error
		l, _ := s.maintenanceLocal()
		if s.cfg.Role == "client" {
			v, e = s.maintenanceRemote(ctx, nil)
		} else {
			v, e = s.maintenanceState()
		}
		if e != nil {
			fail(w, 503, e.Error())
			return
		}
		_, workerErr := os.Stat("/etc/systemd/system/familytunnel-maintenance.timer")
		jsonOut(w, 200, map[string]any{"role": s.cfg.Role, "state": v, "local_error": l.Error, "worker_installed": workerErr == nil})
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "GET or POST required")
		return
	}
	if s.cfg.Role != "client" {
		fail(w, 409, "Zeitplan und Tests bitte in der Verwaltung des Heim-CT einstellen")
		return
	}
	var in maintenanceInput
	if decode(w, r, &in) != nil {
		return
	}
	if in.Action != "settings" && in.Action != "test" {
		fail(w, 400, "Unbekannte Aktion")
		return
	}
	l, e := s.maintenanceLocal()
	if e != nil {
		fail(w, 500, "Lokale Wartungsdaten nicht lesbar")
		return
	}
	if in.Action == "settings" {
		in.Plan.Generation = random(18)
		if e = in.Plan.validate(); e != nil {
			fail(w, 400, e.Error())
			return
		}
		l.Plan = in.Plan
	} else {
		if in.Confirmation != "RESTART_BOTH" {
			fail(w, 400, "Bestätigung fehlt")
			return
		}
		in.Test = random(18)
		in.Boot = s.maintenanceRuntime().Boot()
		l.Test = in.Test
	}
	// Persist local opt-in before the remote worker can see this request.
	l.Error = ""
	if e = s.saveMaintenanceLocal(l); e != nil {
		fail(w, 500, "Lokale Zustimmung konnte nicht gespeichert werden")
		return
	}
	v, e := s.maintenanceRemote(ctx, &in)
	if e != nil {
		fail(w, 503, e.Error())
		return
	}
	event(s.db, "maintenance", "Neustart-Einstellungen oder Test an Oracle übertragen")
	jsonOut(w, 200, map[string]any{"state": v, "message": "Gespeichert. Der Wartungsdienst übernimmt den Ablauf automatisch."})
}
func maintenanceDue(v *maintenanceState, now time.Time) bool {
	p := v.Plan
	if !p.Enabled || p.validate() != nil {
		return false
	}
	if p.Mode == "interval24" {
		if p.Next.IsZero() {
			v.Plan.Next = nextMaintenanceTime(p, now)
			return false
		}
		if now.Before(p.Next) {
			return false
		}
		due := now.Sub(p.Next) < 90*time.Second
		for !v.Plan.Next.After(now) {
			v.Plan.Next = v.Plan.Next.Add(24 * time.Hour)
		}
		return due
	}
	loc, _ := time.LoadLocation(p.Zone)
	local := now.In(loc)
	slot := local.Format("2006-01-02")
	if local.Format("15:04") != p.Time || v.LastSlot == slot {
		return false
	}
	v.LastSlot = slot
	return true
}
func (s *Service) maintenanceServerTick(ctx context.Context) error {
	rt := s.maintenanceRuntime()
	now := rt.Now().UTC()
	boot := rt.Boot()
	if !maintenanceBoot.MatchString(boot) {
		return errors.New("Boot-Kennung nicht verfügbar")
	}
	ready := rt.Ready(ctx)
	uptime := rt.Uptime()
	var action string
	var jobID string
	e := s.mutateMaintenance("maintenance_state", func(v *maintenanceState) error {
		action = ""
		jobID = ""
		fresh := !v.Seen.IsZero() && now.Sub(v.Seen) >= 0 && now.Sub(v.Seen) < 90*time.Second && v.ClientReady
		if !maintenanceActive(v.Job) && maintenanceDue(v, now) {
			if !fresh || v.ClientGrant != v.Plan.Generation || uptime < maintenanceStableFor {
				return nil
			}
			v.Job = &maintenanceJob{ID: random(18), Grant: v.Plan.Generation, Source: "schedule", State: "requested", Created: now, Expires: now.Add(20 * time.Minute), ClientBefore: v.ClientBoot, Message: "Geplanter Neustart vorbereitet."}
		}
		j := v.Job
		if !maintenanceActive(j) {
			return nil
		}
		jobID = j.ID
		if !now.Before(j.Expires) {
			j.State = "failed"
			j.Message = "Wartungsfenster abgelaufen. Keine weiteren Neustarts."
			return nil
		}
		consent := (j.Source == "test" && v.TestGrant == j.Grant) || (j.Source == "schedule" && v.Plan.Enabled && v.ClientGrant == j.Grant)
		if j.State == "requested" {
			if !fresh || !consent || !ready {
				return nil
			}
			if v.ClientBoot != j.ClientBefore {
				j.State = "failed"
				j.Message = "CT wurde vorzeitig neu gestartet; Auftrag abgebrochen."
				return nil
			}
			j.ServerBefore = boot
			j.State = "server_rebooting"
			j.Message = "Oracle-Neustart dauerhaft reserviert. Danach wird auf stabile Dienste gewartet."
			action = "reboot"
			return nil
		}
		if j.State == "server_rebooting" {
			if boot == j.ServerBefore {
				return nil
			}
			j.ServerAfter = boot
			// Start stability monitoring as soon as the rebooted services are ready;
			// do not add a separate uptime delay before this one-minute interval.
			if !ready || !fresh || !consent {
				j.StableSince = time.Time{}
				return nil
			}
			if j.StableSince.IsZero() || j.ReadyAt.IsZero() || now.Sub(j.ReadyAt) > 45*time.Second || now.Sub(j.ReadyAt) < 0 {
				j.StableSince = now
				j.ReadyAt = now
				return nil
			}
			j.ReadyAt = now
			if now.Sub(j.StableSince) >= maintenanceStableFor {
				j.State = "waiting_client"
				j.ReadyAt = now
				j.Message = "Oracle wurde neu gestartet und ist seit einer Minute stabil. CT-Neustart freigegeben."
			}
		}
		if j.State == "waiting_client" {
			if !ready || !fresh || !consent || boot != j.ServerAfter || uptime < maintenanceStableFor {
				j.State = "server_rebooting"
				j.StableSince = time.Time{}
				j.ReadyAt = time.Time{}
			} else {
				j.ReadyAt = now
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	if action == "reboot" {
		event(s.db, "maintenance", "Oracle-Neustart angefordert")
		if e = rt.Reboot(ctx); e != nil {
			_ = s.mutateMaintenance("maintenance_state", func(v *maintenanceState) error {
				if v.Job != nil && v.Job.ID == jobID {
					v.Job.State = "failed"
					v.Job.Message = "Oracle-Neustart fehlgeschlagen. Kein automatischer Wiederholungsversuch."
				}
				return nil
			})
			return errors.New("Oracle-Neustart fehlgeschlagen")
		}
	}
	return nil
}
func (s *Service) maintenanceClientTick(ctx context.Context) error {
	rt := s.maintenanceRuntime()
	boot := rt.Boot()
	if !maintenanceBoot.MatchString(boot) {
		return errors.New("Boot-Kennung nicht verfügbar")
	}
	l, e := s.maintenanceLocal()
	if e != nil {
		return e
	}
	grant := ""
	if l.Plan.Enabled {
		grant = l.Plan.Generation
	}
	v, e := s.maintenanceRemote(ctx, &maintenanceInput{Action: "heartbeat", Boot: boot, Grant: grant, Test: l.Test, Ready: rt.Ready(ctx)})
	if e != nil {
		return e
	}
	j := v.Job
	if j == nil || j.State != "waiting_client" || !rt.Now().Before(j.Expires) || j.ClientBefore != boot || j.ServerAfter == "" || j.ServerBefore == j.ServerAfter {
		return nil
	}
	consent := (j.Source == "test" && l.Test == j.Grant) || (j.Source == "schedule" && l.Plan.Enabled && l.Plan.Generation == j.Grant)
	if !consent || !rt.Ready(ctx) || l.Consumed == j.ID {
		return nil
	}
	// An atomic conditional write across independent workers prevents double reboot.
	old := setting(s.db, "maintenance_local")
	var latest maintenanceLocal
	if json.Unmarshal([]byte(old), &latest) != nil {
		return errors.New("Zustimmung fehlt")
	}
	if !((j.Source == "test" && latest.Test == j.Grant) || (j.Source == "schedule" && latest.Plan.Enabled && latest.Plan.Generation == j.Grant)) {
		return nil
	}
	if latest.Consumed == j.ID {
		return nil
	}
	latest.Consumed = j.ID
	latest.Error = ""
	b, _ := json.Marshal(latest)
	result, e := s.db.Exec("UPDATE settings SET v=? WHERE k='maintenance_local' AND v=?", string(b), old)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return nil
	}
	_, e = s.maintenanceRemote(ctx, &maintenanceInput{Action: "ack", ID: j.ID, Boot: boot})
	if e != nil {
		return e
	}
	event(s.db, "maintenance", "FamilyTunnel-CT-Neustart angefordert")
	if e = rt.Reboot(ctx); e != nil {
		_, _ = s.maintenanceRemote(ctx, &maintenanceInput{Action: "fail", ID: j.ID})
		return errors.New("CT-Neustart fehlgeschlagen")
	}
	return nil
}
func runMaintenance(c Config) error {
	db, e := openDB(filepath.Join(c.Dir, "state.db"))
	if e != nil {
		return e
	}
	defer db.Close()
	s := &Service{cfg: c, db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if c.Role == "server" {
		return s.maintenanceServerTick(ctx)
	}
	return s.maintenanceClientTick(ctx)
}
