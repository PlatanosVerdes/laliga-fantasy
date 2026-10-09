package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The modules live in folders and import each other by path, so a nested asset is served and
// nothing outside the folder is.
func TestAssetsServeFoldersAndNothingAbove(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(filepath.Join(assets, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(assets, "ui", "main.js"), []byte("export {}"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "secret.txt"), []byte("no"), 0o644)
	server := &Server{opts: Options{Assets: assets}}
	for path, want := range map[string]int{
		"/assets/ui/main.js":          http.StatusOK,
		"/assets/ui/":                 http.StatusNotFound,
		"/assets/../secret.txt":       http.StatusNotFound,
		"/assets/ui/../../secret.txt": http.StatusNotFound,
	} {
		recorder := httptest.NewRecorder()
		server.asset(recorder, httptest.NewRequest(http.MethodGet, "http://x"+path, nil))
		if recorder.Code != want {
			t.Errorf("%s: %d, want %d", path, recorder.Code, want)
		}
		if want == http.StatusOK && recorder.Header().Get("Content-Type") !=
			"application/javascript; charset=utf-8" {
			t.Errorf("%s: %s", path, recorder.Header().Get("Content-Type"))
		}
	}
}
