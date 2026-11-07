package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouteSaveDoesNotWaitForOracle(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db, cfg: Config{Role: "client", ServerURL: "://invalid"}, routeChanges: make(chan struct{}, 1)}
	// A health sync may hold this network lock for seconds. CRUD must remain available.
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, test := range []struct {
		method, path, body string
		count              int
	}{
		{"POST", "/api/routes", `{"domain":"a.example.net","backend":"http://192.168.1.2:8096","enabled":true}`, 1},
		{"PUT", "/api/routes/1", `{"domain":"b.example.net","backend":"http://192.168.1.3:8096","public_port":8096,"enabled":true}`, 1},
		{"DELETE", "/api/routes/1", "", 0},
	} {
		out := httptest.NewRecorder()
		s.routeAPI(out, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
		if out.Code != http.StatusAccepted {
			t.Fatalf("%s: %d %s", test.method, out.Code, out.Body.String())
		}
		var response struct {
			Routes  []Route `json:"routes"`
			Pending bool    `json:"sync_pending"`
		}
		if err = json.Unmarshal(out.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Pending || len(response.Routes) != test.count {
			t.Fatalf("wrong save response: %+v", response)
		}
	}
	if len(s.routeChanges) != 1 {
		t.Fatal("sync requests must coalesce into one pending signal")
	}
}
