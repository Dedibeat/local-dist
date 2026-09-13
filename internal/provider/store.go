package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type Catalog struct {
	SchemaVersion int       `json:"schemaVersion"`
	Packages      []Package `json:"packages"`
}

type Package struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Type        string     `json:"type"`
	Source      string     `json:"source"`
	UpstreamURL string     `json:"upstreamUrl,omitempty"`
	SHA256      string     `json:"sha256"`
	Install     Install    `json:"install"`
	Detect      *Detection `json:"detect,omitempty"`
	DownloadURL string     `json:"downloadUrl,omitempty"`
	Available   bool       `json:"available"`
}

type Install struct {
	Args        []string        `json:"args,omitempty"`
	Destination string          `json:"destination,omitempty"`
	AddToPath   []string        `json:"addToPath,omitempty"`
	StartMenu   *StartMenuEntry `json:"startMenu,omitempty"`
}

type StartMenuEntry struct {
	Folder string `json:"folder"`
	Name   string `json:"name"`
}

type Detection struct {
	Type string   `json:"type"`
	Path string   `json:"path"`
	Args []string `json:"args,omitempty"`
}

type RoomsFile struct {
	SchemaVersion int    `json:"schemaVersion"`
	Rooms         []Room `json:"rooms"`
}

type Room struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Complete   bool     `json:"complete"`
	PackageIDs []string `json:"packages"`
	Pending    []string `json:"pending,omitempty"`
}

type RoomPlan struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Complete bool      `json:"complete"`
	Packages []Package `json:"packages"`
	Pending  []string  `json:"pending,omitempty"`
}

type Store struct {
	DataDir  string
	Catalog  Catalog
	Rooms    []Room
	packages map[string]Package
	rooms    map[string]Room
}

func Load(dataDir string) (*Store, error) {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}

	var catalog Catalog
	if err := readJSON(filepath.Join(abs, "catalog.json"), &catalog); err != nil {
		return nil, err
	}
	var roomsFile RoomsFile
	if err := readJSON(filepath.Join(abs, "rooms.json"), &roomsFile); err != nil {
		return nil, err
	}
	if catalog.SchemaVersion != 1 || roomsFile.SchemaVersion != 1 {
		return nil, fmt.Errorf("only schemaVersion 1 is supported")
	}
	if catalog.Packages == nil {
		catalog.Packages = []Package{}
	}

	store := &Store{
		DataDir:  abs,
		Catalog:  catalog,
		Rooms:    roomsFile.Rooms,
		packages: make(map[string]Package),
		rooms:    make(map[string]Room),
	}

	for i := range store.Catalog.Packages {
		pkg := &store.Catalog.Packages[i]
		if err := validatePackage(*pkg); err != nil {
			return nil, fmt.Errorf("package %q: %w", pkg.ID, err)
		}
		if _, exists := store.packages[pkg.ID]; exists {
			return nil, fmt.Errorf("duplicate package id %q", pkg.ID)
		}
		pkg.DownloadURL = (&url.URL{Path: "/files/" + pkg.Source}).EscapedPath()
		pkg.Available = store.packageAvailable(*pkg)
		store.packages[pkg.ID] = *pkg
	}

	for _, room := range store.Rooms {
		if !idPattern.MatchString(room.ID) {
			return nil, fmt.Errorf("invalid room id %q", room.ID)
		}
		if _, exists := store.rooms[room.ID]; exists {
			return nil, fmt.Errorf("duplicate room id %q", room.ID)
		}
		if room.Complete && len(room.Pending) > 0 {
			return nil, fmt.Errorf("room %q is complete but still has pending requirements", room.ID)
		}
		seenPackages := make(map[string]struct{}, len(room.PackageIDs))
		for _, packageID := range room.PackageIDs {
			if _, exists := store.packages[packageID]; !exists {
				return nil, fmt.Errorf("room %q refers to unknown package %q", room.ID, packageID)
			}
			if _, exists := seenPackages[packageID]; exists {
				return nil, fmt.Errorf("room %q contains package %q more than once", room.ID, packageID)
			}
			seenPackages[packageID] = struct{}{}
		}
		store.rooms[room.ID] = room
	}

	return store, nil
}

func (s *Store) RoomPlan(id string) (RoomPlan, bool) {
	room, ok := s.rooms[id]
	if !ok {
		return RoomPlan{}, false
	}
	plan := RoomPlan{
		ID:       room.ID,
		Name:     room.Name,
		Complete: room.Complete,
		Packages: make([]Package, 0, len(room.PackageIDs)),
		Pending:  room.Pending,
	}
	for _, packageID := range room.PackageIDs {
		pkg := s.packages[packageID]
		pkg.Available = s.packageAvailable(pkg)
		plan.Packages = append(plan.Packages, pkg)
	}
	return plan, true
}

func validatePackage(pkg Package) error {
	if !idPattern.MatchString(pkg.ID) {
		return fmt.Errorf("invalid id")
	}
	if pkg.Name == "" || pkg.Version == "" {
		return fmt.Errorf("name and version are required")
	}
	if pkg.Type != "msi" && pkg.Type != "exe" && pkg.Type != "zip" && pkg.Type != "portable" && pkg.Type != "npm" {
		return fmt.Errorf("type must be msi, exe, zip, portable, or npm")
	}
	if err := validateSource(pkg.Source); err != nil {
		return err
	}
	if len(pkg.SHA256) != sha256.Size*2 {
		return fmt.Errorf("sha256 must contain 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(pkg.SHA256); err != nil {
		return fmt.Errorf("invalid sha256: %w", err)
	}
	if (pkg.Type == "zip" || pkg.Type == "portable") && pkg.Install.Destination == "" {
		return fmt.Errorf("install.destination is required for %s packages", pkg.Type)
	}
	if pkg.Detect != nil {
		if pkg.Detect.Type != "file" && pkg.Detect.Type != "command" {
			return fmt.Errorf("detect.type must be file or command")
		}
		if pkg.Detect.Path == "" {
			return fmt.Errorf("detect.path must not be empty")
		}
		if pkg.Detect.Type == "file" && len(pkg.Detect.Args) > 0 {
			return fmt.Errorf("detect.args is supported only for command detection")
		}
	}
	if pkg.Install.StartMenu != nil {
		if pkg.Detect == nil || pkg.Detect.Type != "file" {
			return fmt.Errorf("install.startMenu requires a file detect rule")
		}
		if err := validateStartMenuPart("folder", pkg.Install.StartMenu.Folder); err != nil {
			return err
		}
		if err := validateStartMenuPart("name", pkg.Install.StartMenu.Name); err != nil {
			return err
		}
		if !strings.HasSuffix(strings.ToLower(pkg.Install.StartMenu.Name), ".lnk") {
			return fmt.Errorf("install.startMenu.name must end in .lnk")
		}
	}
	return nil
}

func validateStartMenuPart(field, value string) error {
	if strings.TrimSpace(value) == "" || value == "." || value == ".." ||
		strings.ContainsAny(value, `/\\:*?"<>|`) ||
		strings.IndexFunc(value, func(r rune) bool { return r < 32 }) >= 0 {
		return fmt.Errorf("install.startMenu.%s must be a single safe filename part", field)
	}
	return nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("decode %s: expected exactly one JSON value", path)
	}
	return nil
}

func validateSource(source string) error {
	if !strings.HasPrefix(source, "packages/") || path.Clean(source) != source ||
		strings.ContainsAny(source, "\\\x00:") {
		return fmt.Errorf("source must be a canonical slash-separated path below packages/")
	}
	if strings.ContainsAny(path.Base(source), `<>"|?*`) ||
		strings.IndexFunc(source, func(r rune) bool { return r < 32 }) >= 0 {
		return fmt.Errorf("source must have a filename usable by Windows clients and contain no control characters")
	}
	return nil
}

// packagePath rejects symlinks and special files. Package directories are owned
// by the administrator and must not be writable by untrusted users.
func (s *Store) packagePath(source string) (string, error) {
	if err := validateSource(source); err != nil {
		return "", err
	}
	current := s.DataDir
	parts := strings.Split(source, "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return filepath.Join(s.DataDir, filepath.FromSlash(source)), nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) ||
			(i == len(parts)-1 && !info.Mode().IsRegular()) {
			return "", fmt.Errorf("package path %q must contain only directories and a regular file", source)
		}
	}
	return current, nil
}

func (s *Store) packageAvailable(pkg Package) bool {
	filename, err := s.packagePath(pkg.Source)
	if err != nil {
		return false
	}
	info, err := os.Stat(filename)
	return err == nil && info.Mode().IsRegular()
}
