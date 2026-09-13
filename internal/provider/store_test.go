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

func TestValidatePackageAcceptsNPM(t *testing.T) {
	pkg := Package{
		ID: "typescript", Name: "TypeScript", Version: "1.0", Type: "npm",
		Source: "packages/typescript/typescript-1.0.tgz", SHA256: strings.Repeat("0", 64),
	}
	if err := validatePackage(pkg); err != nil {
		t.Fatalf("validate npm package: %v", err)
	}
}

func TestValidatePackageDetection(t *testing.T) {
	base := Package{
		ID: "demo", Name: "Demo", Version: "1.0", Type: "exe",
		Source: "packages/demo/setup.exe", SHA256: strings.Repeat("0", 64),
	}

	for _, detection := range []*Detection{
		{Type: "file", Path: `C:\Program Files\Demo\demo.exe`},
		{Type: "command", Path: `%APPDATA%\Demo\demo.cmd`, Args: []string{"--version"}},
	} {
		pkg := base
		pkg.Detect = detection
		if err := validatePackage(pkg); err != nil {
			t.Fatalf("rejected detection %#v: %v", detection, err)
		}
	}

	for _, detection := range []*Detection{
		{Type: "registry", Path: `HKLM:\Software\Demo`},
		{Type: "file"},
		{Type: "file", Path: `C:\Demo.exe`, Args: []string{"--version"}},
	} {
		pkg := base
		pkg.Detect = detection
		if err := validatePackage(pkg); err == nil {
			t.Fatalf("accepted invalid detection %#v", detection)
		}
	}
}

func TestValidatePackageStartMenu(t *testing.T) {
	base := Package{
		ID: "demo", Name: "Demo", Version: "1.0", Type: "exe",
		Source: "packages/demo/setup.exe", SHA256: strings.Repeat("0", 64),
		Detect: &Detection{Type: "file", Path: `C:\Program Files\Demo\demo.exe`},
	}
	base.Install.StartMenu = &StartMenuEntry{Folder: "Demo Tools", Name: "Demo.lnk"}
	if err := validatePackage(base); err != nil {
		t.Fatalf("validate start menu entry: %v", err)
	}

	for _, entry := range []*StartMenuEntry{
		{Folder: "", Name: "Demo.lnk"},
		{Folder: "..", Name: "Demo.lnk"},
		{Folder: "Demo\\Tools", Name: "Demo.lnk"},
		{Folder: "Demo Tools", Name: "Demo.exe"},
	} {
		pkg := base
		pkg.Install.StartMenu = entry
		if err := validatePackage(pkg); err == nil {
			t.Fatalf("accepted unsafe start menu entry %#v", entry)
		}
	}
}

func TestValidatePackageUninstallBeforeInstall(t *testing.T) {
	base := Package{
		ID: "demo", Name: "Demo", Version: "1.0", Type: "msi",
		Source: "packages/demo/setup.msi", SHA256: strings.Repeat("0", 64),
	}
	base.Install.UninstallBeforeInstall = "{2512FA64-8592-4C98-8430-9262623F95F0}"
	if err := validatePackage(base); err != nil {
		t.Fatalf("validate MSI replacement: %v", err)
	}

	for _, test := range []struct {
		packageType string
		productCode string
	}{
		{"exe", "{2512FA64-8592-4C98-8430-9262623F95F0}"},
		{"msi", "2512FA64-8592-4C98-8430-9262623F95F0"},
		{"msi", "{not-a-product-code}"},
	} {
		pkg := base
		pkg.Type = test.packageType
		pkg.Install.UninstallBeforeInstall = test.productCode
		if err := validatePackage(pkg); err == nil {
			t.Fatalf("accepted %s replacement product code %q", test.packageType, test.productCode)
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
