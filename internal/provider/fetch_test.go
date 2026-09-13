package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchPackagesDownloadsAndVerifiesMissingFile(t *testing.T) {
	payload := []byte("trusted installer")
	hash := sha256.Sum256(payload)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dataDir := t.TempDir()
	store := &Store{
		DataDir: dataDir,
		Catalog: Catalog{Packages: []Package{{
			ID:          "demo",
			Name:        "Demo",
			Version:     "1.0",
			Source:      "packages/demo/setup.exe",
			UpstreamURL: server.URL,
			SHA256:      hex.EncodeToString(hash[:]),
		}}},
	}

	var output strings.Builder
	if err := FetchPackages(context.Background(), store, server.Client(), &output); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dataDir, "packages", "demo", "setup.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(payload) {
		t.Fatalf("unexpected contents: %q", contents)
	}
	if !strings.Contains(output.String(), "Downloading: Demo 1.0") {
		t.Fatalf("unexpected output: %q", output.String())
	}
}

func TestFetchPackagesRejectsBadDownload(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "wrong file")
	}))
	defer server.Close()

	dataDir := t.TempDir()
	store := &Store{
		DataDir: dataDir,
		Catalog: Catalog{Packages: []Package{{
			ID:          "demo",
			Name:        "Demo",
			Version:     "1.0",
			Source:      "packages/demo/setup.exe",
			UpstreamURL: server.URL,
			SHA256:      strings.Repeat("0", 64),
		}}},
	}

	err := FetchPackages(context.Background(), store, server.Client(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("expected checksum error, got %v", err)
	}
	entries, readErr := os.ReadDir(filepath.Join(dataDir, "packages", "demo"))
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("failed download left files: %v, %v", entries, readErr)
	}
}

func TestConcurrentFetches(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
			_, _ = io.WriteString(w, "installer")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	store := testStore(t, "packages/demo/setup.exe")
	hash := sha256.Sum256([]byte("installer"))
	store.Catalog.Packages[0].SHA256 = hex.EncodeToString(hash[:])
	store.Catalog.Packages[0].UpstreamURL = server.URL
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errors <- FetchPackages(ctx, store, server.Client(), io.Discard) }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("both concurrent downloads did not start")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(store.DataDir, "packages", "demo"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "setup.exe" {
		t.Fatalf("unexpected published files: %v, %v", entries, err)
	}
}

func TestFetchDoesNotOverwriteFileCreatedDuringDownload(t *testing.T) {
	store := testStore(t, "packages/demo/setup.exe")
	target := filepath.Join(store.DataDir, "packages", "demo", "setup.exe")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := os.WriteFile(target, []byte("another installer"), 0o644); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, "installer")
	}))
	defer server.Close()
	hash := sha256.Sum256([]byte("installer"))
	store.Catalog.Packages[0].SHA256 = hex.EncodeToString(hash[:])
	store.Catalog.Packages[0].UpstreamURL = server.URL
	if err := FetchPackages(context.Background(), store, server.Client(), io.Discard); err == nil {
		t.Fatal("expected conflict with existing file")
	}
	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "another installer" {
		t.Fatalf("existing installer was overwritten: %q, %v", contents, err)
	}
}

func TestFetchRejectsHTTPRedirect(t *testing.T) {
	insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("followed an insecure redirect")
	}))
	defer insecure.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, insecure.URL, http.StatusFound)
	}))
	defer secure.Close()
	store := testStore(t, "packages/demo/setup.exe")
	store.Catalog.Packages[0].UpstreamURL = secure.URL
	err := FetchPackages(context.Background(), store, secure.Client(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "redirect must use HTTPS") {
		t.Fatalf("expected HTTPS redirect error, got %v", err)
	}
}

func TestFetchRejectsSymlinkDirectory(t *testing.T) {
	store := testStore(t, "packages/demo/setup.exe")
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(store.DataDir, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store.DataDir, "packages", "demo")); err != nil {
		t.Fatal(err)
	}
	err := FetchPackages(context.Background(), store, http.DefaultClient, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "package path") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside the package directory: %v, %v", entries, err)
	}
}
