package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRoomPlanAndDownload(t *testing.T) {
	dataDir := t.TempDir()
	mustWrite(t, filepath.Join(dataDir, "catalog.json"), `{
  "schemaVersion": 1,
  "packages": [{
    "id": "demo", "name": "Demo", "version": "1.0", "type": "exe",
    "source": "packages/demo/setup.exe",
    "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    "install": {"args": ["/S"], "startMenu": {"folder": "Demo Tools", "name": "Demo.lnk"}},
    "detect": {"type": "file", "path": "C:\\Program Files\\Demo\\demo.exe"}
  }]
}`)
	mustWrite(t, filepath.Join(dataDir, "rooms.json"), `{
  "schemaVersion": 1,
  "rooms": [{"id": "room-302", "name": "Room 302", "complete": false, "packages": ["demo"], "pending": ["Another tool"]}]
}`)
	mustWrite(t, filepath.Join(dataDir, "packages", "demo", "setup.exe"), "installer")

	store, err := Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(store))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/rooms/room-302")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected room status: %d", response.StatusCode)
	}
	var plan RoomPlan
	if err := json.NewDecoder(response.Body).Decode(&plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Packages) != 1 || !plan.Packages[0].Available {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if plan.Packages[0].Install.StartMenu == nil || plan.Packages[0].Install.StartMenu.Name != "Demo.lnk" {
		t.Fatalf("start menu metadata was not returned: %#v", plan.Packages[0].Install)
	}
	if plan.Complete || len(plan.Pending) != 1 {
		t.Fatalf("unexpected completion state: %#v", plan)
	}

	download, err := http.Get(server.URL + plan.Packages[0].DownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusOK {
		t.Fatalf("unexpected download status: %d", download.StatusCode)
	}
}

func TestRejectsUnknownRoomPackage(t *testing.T) {
	dataDir := t.TempDir()
	mustWrite(t, filepath.Join(dataDir, "catalog.json"), `{"schemaVersion":1,"packages":[]}`)
	mustWrite(t, filepath.Join(dataDir, "rooms.json"), `{"schemaVersion":1,"rooms":[{"id":"lab","name":"Lab","packages":["missing"]}]}`)
	if _, err := Load(dataDir); err == nil {
		t.Fatal("expected invalid room package reference to fail")
	}
}

func TestDoesNotListDirectories(t *testing.T) {
	dataDir := t.TempDir()
	mustWrite(t, filepath.Join(dataDir, "catalog.json"), `{"schemaVersion":1,"packages":[]}`)
	mustWrite(t, filepath.Join(dataDir, "rooms.json"), `{"schemaVersion":1,"rooms":[]}`)
	store, err := Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/files/packages/", nil)
	NewHandler(store).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}

func TestFilesEndpointDoesNotExposeConfiguration(t *testing.T) {
	dataDir := t.TempDir()
	mustWrite(t, filepath.Join(dataDir, "catalog.json"), `{"schemaVersion":1,"packages":[]}`)
	mustWrite(t, filepath.Join(dataDir, "rooms.json"), `{"schemaVersion":1,"rooms":[]}`)
	store, err := Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/files/catalog.json", nil)
	NewHandler(store).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}

func TestDownloadsOnlyCatalogFiles(t *testing.T) {
	store := testStore(t, "packages/demo/setup.exe")
	mustWrite(t, filepath.Join(store.DataDir, "packages", "unlisted.exe"), "private")
	mustWrite(t, filepath.Join(store.DataDir, "packages", "demo", "setup.exe.part"), "partial")
	if err := os.MkdirAll(filepath.Join(store.DataDir, "packages", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(store.DataDir, "catalog.json"), filepath.Join(store.DataDir, "packages", "demo", "setup.exe")); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(store)
	for _, target := range []string{
		"/files/packages/unlisted.exe", "/files/packages/demo/setup.exe.part",
		"/files/packages/demo/setup.exe", "/files/packages/%2e%2e/catalog.json",
		"/files/packages/..%2fcatalog.json", "/files/packages/demo", "/files/packages/demo/",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", target, recorder.Code)
		}
	}
}

func TestDownloadEscapedFilenameAndRange(t *testing.T) {
	store := testStore(t, "packages/demo/setup #1%.exe")
	mustWrite(t, filepath.Join(store.DataDir, "packages", "demo", "setup #1%.exe"), "installer")
	server := httptest.NewServer(NewHandler(store))
	defer server.Close()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		request, err := http.NewRequest(method, server.URL+store.Catalog.Packages[0].DownloadURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if method == http.MethodGet {
			request.Header.Set("Range", "bytes=0-3")
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if method == http.MethodGet && (response.StatusCode != http.StatusPartialContent || string(body) != "inst") {
			t.Fatalf("range download: status %d, body %q", response.StatusCode, body)
		}
		if method == http.MethodHead && (response.StatusCode != http.StatusOK || len(body) != 0) {
			t.Fatalf("HEAD download: status %d, body %q", response.StatusCode, body)
		}
	}
}

func TestAvailabilityTracksFilesWithoutRestart(t *testing.T) {
	store := testStore(t, "packages/demo/setup.exe")
	handler := NewHandler(store)
	filename := filepath.Join(store.DataDir, "packages", "demo", "setup.exe")
	for _, available := range []bool{false, true, false} {
		if available {
			mustWrite(t, filename, "installer")
		} else if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		plan, _ := store.RoomPlan("lab")
		if plan.Packages[0].Available != available {
			t.Fatalf("room availability: got %v, want %v", plan.Packages[0].Available, available)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil))
		var catalog Catalog
		if err := json.Unmarshal(recorder.Body.Bytes(), &catalog); err != nil {
			t.Fatal(err)
		}
		if catalog.Packages[0].Available != available {
			t.Fatalf("catalog availability: got %v, want %v", catalog.Packages[0].Available, available)
		}
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
