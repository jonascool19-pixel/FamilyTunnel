package app

import (
	"path/filepath"
	"testing"
)

func TestRouteValidation(t *testing.T) {
	good := Route{Domain: "home.example.com", Backend: "http://192.168.1.20:8080", Enabled: true}
	if e := good.validate(); e != nil {
		t.Fatal(e)
	}
	bad := []Route{{Domain: "INVALID", Backend: good.Backend}, {Domain: good.Domain, Backend: "http://127.0.0.1:80"}, {Domain: good.Domain, Backend: "http://10.77.0.1:80"}, {Domain: good.Domain, Backend: "file:///etc/passwd"}, {Domain: good.Domain, Backend: "http://192.168.1.20:99999"}}
	for _, r := range bad {
		if e := r.validate(); e == nil {
			t.Fatalf("accepted invalid route: %+v", r)
		}
	}
}
func TestDatabasePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, e := openDB(path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec("INSERT INTO routes(domain,backend,enabled) VALUES(?,?,1)", "home.example.com", "http://192.168.1.20:8080")
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	db, e = openDB(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	rs, e := routes(db)
	if e != nil || len(rs) != 1 || rs[0].Domain != "home.example.com" {
		t.Fatalf("persistence failed: %+v %v", rs, e)
	}
}
func TestReplaceRoutesAtomic(t *testing.T) {
	db, e := openDB(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = replaceRoutes(db, []Route{{Domain: "a.example.com", Backend: "http://192.168.1.2", Enabled: true}}); e != nil {
		t.Fatal(e)
	}
	e = replaceRoutes(db, []Route{{Domain: "bad", Backend: "http://192.168.1.3", Enabled: true}})
	if e == nil {
		t.Fatal("invalid replacement accepted")
	}
	rs, _ := routes(db)
	if len(rs) != 1 || rs[0].Domain != "a.example.com" {
		t.Fatal("atomicity failed")
	}
}
