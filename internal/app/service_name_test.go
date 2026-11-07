package app

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditableServiceNamesPersistAndSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db, cfg: Config{Role: "client"}, routeChanges: make(chan struct{}, 1)}
	original := `{"name":"Jellyfin","domain":"media.example.net","backend":"http://192.168.1.95:8096","public_port":8096,"enabled":true}`
	out := httptest.NewRecorder()
	s.routeAPI(out, httptest.NewRequest("POST", "/api/routes", strings.NewReader(original)))
	if out.Code != 202 {
		t.Fatal(out.Body.String())
	}
	changed := `{"name":"Mein Medienserver","domain":"tv.example.net","backend":"http://192.168.1.96:8097","public_port":8098,"enabled":true}`
	out = httptest.NewRecorder()
	s.routeAPI(out, httptest.NewRequest("PUT", "/api/routes/1", strings.NewReader(changed)))
	if out.Code != 202 || !strings.Contains(out.Body.String(), "Mein Medienserver") {
		t.Fatal(out.Body.String())
	}
	db.Close()
	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	web, err := routes(db)
	if err != nil || len(web) != 1 || web[0].Name != "Mein Medienserver" || web[0].Backend != "http://192.168.1.96:8097" {
		t.Fatalf("edit lost: %+v %v", web, err)
	}
	clone, err := openDB(filepath.Join(t.TempDir(), "clone.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	if err = replaceRoutes(clone, web); err != nil {
		t.Fatal(err)
	}
	if err = replaceAllRoutes(clone, web, []PortForward{}); err != nil {
		t.Fatal(err)
	}
	mirrored, _ := routes(clone)
	if len(mirrored) != 1 || mirrored[0].Name != web[0].Name {
		t.Fatal("sync/restore discarded name")
	}
	s.db = db
	bad := httptest.NewRecorder()
	s.routeAPI(bad, httptest.NewRequest("PUT", "/api/routes/1", strings.NewReader(strings.Replace(changed, "Mein Medienserver", strings.Repeat("a", 81), 1))))
	if bad.Code != 400 {
		t.Fatal("overlong name accepted")
	}
	unchanged, _ := routes(db)
	if unchanged[0].Name != "Mein Medienserver" {
		t.Fatal("failed edit partially saved")
	}
}
