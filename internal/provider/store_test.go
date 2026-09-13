package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T, source string) *Store {
	t.Helper()
	dir := t.TempDir()
	catalog := Catalog{SchemaVersion: 1, Packages: []Package{{
		ID: "demo", Name: "Demo", Version: "1", Type: "exe",
		Source: source, SHA256: strings.Repeat("0", 64),
	}}}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "catalog.json"), string(data))
	mustWrite(t, filepath.Join(dir, "rooms.json"), `{"schemaVersion":1,"rooms":[{"id":"lab","packages":["demo"]}]}`)
	store, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestValidateSource(t *testing.T) {
	for _, source := range []string{
		"", "packages", "packages/", "packages/../catalog.json", "packages/a/../../secret",
		"packages/a/../setup.exe", "packages//setup.exe", "packages/./setup.exe",
		"/packages/setup.exe", `packages/..\secret`, "packages/setup.exe:stream", "packages/a\x00.exe",
		"packages/setup?.exe", "packages/setup*.exe", "packages/setup\n.exe",
	} {
		t.Run(source, func(t *testing.T) {
			if err := validateSource(source); err == nil {
				t.Fatalf("accepted unsafe or ambiguous source %q", source)
			}
		})
	}
	for _, source := range []string{"packages/setup.exe", "packages/demo/setup #1%.exe"} {
		if err := validateSource(source); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadJSONRejectsTrailingContent(t *testing.T) {
	for _, suffix := range []string{` {"schemaVersion":2}`, " garbage"} {
		filename := filepath.Join(t.TempDir(), "catalog.json")
		mustWrite(t, filename, `{"schemaVersion":1,"packages":[]}`+suffix)
		if err := readJSON(filename, new(Catalog)); err == nil {
			t.Fatalf("accepted trailing content %q", suffix)
		}
	}
}

func TestPackagePathRejectsSymlinks(t *testing.T) {
	for _, directory := range []bool{false, true} {
		store := testStore(t, "packages/demo/setup.exe")
		outside := t.TempDir()
		mustWrite(t, filepath.Join(outside, "setup.exe"), "private")
		link := filepath.Join(store.DataDir, "packages", "demo")
		target := outside
		if !directory {
			link = filepath.Join(link, "setup.exe")
			target = filepath.Join(outside, "setup.exe")
		}
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := store.packagePath("packages/demo/setup.exe"); err == nil {
			t.Fatal("accepted symlink")
		}
		plan, _ := store.RoomPlan("lab")
		if plan.Packages[0].Available {
			t.Fatal("symlink advertised as available")
		}
	}
}
