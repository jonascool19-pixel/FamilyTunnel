package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Route struct {
	ID         int64  `json:"id"`
	Name       string `json:"name,omitempty"`
	Domain     string `json:"domain"`
	Backend    string `json:"backend"`
	Enabled    bool   `json:"enabled"`
	PublicPort int    `json:"public_port"`
}

func (r Route) port() int {
	if r.PublicPort == 0 {
		return 443
	}
	return r.PublicPort
}
func (r Route) address() string {
	if r.port() == 443 {
		return r.Domain
	}
	return net.JoinHostPort(r.Domain, strconv.Itoa(r.port()))
}
func (r Route) validPort() bool {
	p := r.port()
	return p == 443 || (p >= 1024 && p <= 65535 && p != 2019 && p != 8443 && p != 9080 && p != 9081)
}

var domainRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func (r Route) validate() error {
	if len(r.Name) > 80 {
		return errors.New("Name ist zu lang (maximal 80 Bytes)")
	}
	if !r.validPort() {
		return errors.New("Öffentlicher Port: 443 oder 1024–65535; 2019, 8443, 9080 und 9081 sind reserviert")
	}
	if len(r.Domain) > 253 || !domainRE.MatchString(r.Domain) {
		return errors.New("invalid lowercase DNS domain")
	}
	u, e := url.Parse(r.Backend)
	if e != nil || u == nil {
		return errors.New("invalid backend")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("backend must be an http(s) origin")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.Equal(net.ParseIP("10.77.0.1")) || ip.Equal(net.ParseIP("10.77.0.2")) {
		return errors.New("backend must be a private LAN IP, not a tunnel or loopback address")
	}
	if p := u.Port(); p != "" {
		v, e := strconv.Atoi(p)
		if e != nil || v < 1 || v > 65535 {
			return errors.New("invalid backend port")
		}
	}
	return nil
}
func openDB(path string) (*sql.DB, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;
 CREATE TABLE IF NOT EXISTS settings(k TEXT PRIMARY KEY,v TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS routes(id INTEGER PRIMARY KEY,domain TEXT UNIQUE NOT NULL,backend TEXT NOT NULL,enabled INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS stats(domain TEXT NOT NULL,hour TEXT NOT NULL,requests INTEGER NOT NULL DEFAULT 0,errors INTEGER NOT NULL DEFAULT 0,rx INTEGER NOT NULL DEFAULT 0,tx INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(domain,hour));
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY,at TEXT NOT NULL,kind TEXT NOT NULL,message TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS port_forwards(id INTEGER PRIMARY KEY,name TEXT NOT NULL,protocol TEXT NOT NULL,target_ip TEXT NOT NULL,target_port INTEGER NOT NULL,public_port INTEGER NOT NULL,enabled INTEGER NOT NULL,UNIQUE(protocol,public_port));
 CREATE TABLE IF NOT EXISTS scan_results(id INTEGER PRIMARY KEY,target_ip TEXT NOT NULL,target_port INTEGER NOT NULL,protocol TEXT NOT NULL,found_at TEXT NOT NULL,UNIQUE(target_ip,target_port,protocol));
 CREATE TABLE IF NOT EXISTS scan_services(target_ip TEXT NOT NULL,target_port INTEGER NOT NULL,name TEXT NOT NULL,backend TEXT NOT NULL,PRIMARY KEY(target_ip,target_port));`)
	if e != nil {
		db.Close()
		return nil, e
	}
	// Migrate the original domain-only table atomically; existing routes stay on HTTPS/443.
	var hasPort int
	e = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('routes') WHERE name='public_port'").Scan(&hasPort)
	if e == nil && hasPort == 0 {
		_, e = db.Exec(`BEGIN IMMEDIATE;
CREATE TABLE routes_ports(id INTEGER PRIMARY KEY,domain TEXT NOT NULL,backend TEXT NOT NULL,enabled INTEGER NOT NULL,public_port INTEGER NOT NULL DEFAULT 443,UNIQUE(domain,public_port));
INSERT INTO routes_ports(id,domain,backend,enabled) SELECT id,domain,backend,enabled FROM routes;
DROP TABLE routes;
ALTER TABLE routes_ports RENAME TO routes;
COMMIT;`)
	}
	if e != nil {
		db.Exec("ROLLBACK")
		db.Close()
		return nil, e
	}
	var hasName int
	e = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('routes') WHERE name='name'").Scan(&hasName)
	if e == nil && hasName == 0 {
		_, e = db.Exec("ALTER TABLE routes ADD COLUMN name TEXT NOT NULL DEFAULT ''")
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
func routes(db *sql.DB) ([]Route, error) {
	rows, e := db.Query("SELECT id,domain,backend,enabled,public_port,name FROM routes ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Route{}
	for rows.Next() {
		var r Route
		if e = rows.Scan(&r.ID, &r.Domain, &r.Backend, &r.Enabled, &r.PublicPort, &r.Name); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func setting(db *sql.DB, k string) string {
	var v string
	_ = db.QueryRow("SELECT v FROM settings WHERE k=?", k).Scan(&v)
	return v
}
func setSetting(db *sql.DB, k, v string) error {
	_, e := db.Exec("INSERT INTO settings(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v", k, v)
	return e
}
func event(db *sql.DB, kind, msg string) {
	_, _ = db.Exec("INSERT INTO events(at,kind,message) VALUES(?,?,?)", time.Now().UTC().Format(time.RFC3339), kind, msg)
	_, _ = db.Exec("DELETE FROM events WHERE id < (SELECT COALESCE(MAX(id),0)-1000 FROM events)")
}
func statsJSON(db *sql.DB, period string) (any, error) {
	format := "%Y-%m-%dT%H:00:00Z"
	switch period {
	case "day":
		format = "%Y-%m-%d"
	case "month":
		format = "%Y-%m"
	case "total":
		format = "total"
	case "hour", "":
	default:
		return nil, errors.New("invalid period")
	}
	rows, e := db.Query(`SELECT domain,strftime(?,hour) AS bucket,SUM(requests),SUM(errors),SUM(rx),SUM(tx) FROM stats GROUP BY domain,bucket ORDER BY bucket DESC LIMIT 10000`, format)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var domain, bucket string
		var req, fail, rx, tx int64
		if e = rows.Scan(&domain, &bucket, &req, &fail, &rx, &tx); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"domain": domain, "period": bucket, "requests": req, "errors": fail, "received_bytes": rx, "sent_bytes": tx})
	}
	return out, rows.Err()
}
func replaceRoutes(db *sql.DB, rs []Route) error {
	if len(rs) > 200 {
		return errors.New("maximum 200 routes")
	}
	seen := map[string]bool{}
	for _, r := range rs {
		if e := r.validate(); e != nil {
			return e
		}
		if seen[r.address()] {
			return errors.New("duplicate domain and public port")
		}
		seen[r.address()] = true
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM routes"); e != nil {
		return e
	}
	for _, r := range rs {
		if _, e = tx.Exec("INSERT INTO routes(id,domain,backend,enabled,public_port,name) VALUES(?,?,?,?,?,?)", r.ID, r.Domain, r.Backend, r.Enabled, r.port(), r.Name); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func decodeRoutes(b []byte) ([]Route, error) { var r []Route; e := json.Unmarshal(b, &r); return r, e }
func hostOnly(s string) string {
	if h, _, e := net.SplitHostPort(s); e == nil {
		return h
	}
	return strings.Trim(s, "[]")
}
