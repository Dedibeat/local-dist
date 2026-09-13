package provider

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

type api struct {
	store *Store
}

func NewHandler(store *Store) http.Handler {
	a := &api{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /api/v1/catalog", a.catalog)
	mux.HandleFunc("GET /api/v1/rooms/{id}", a.room)
	mux.HandleFunc("GET /files/packages/", a.download)
	return securityHeaders(mux)
}

func (a *api) index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "local-dist",
		"version": 1,
		"endpoints": map[string]string{
			"catalog": "/api/v1/catalog",
			"room":    "/api/v1/rooms/{id}",
			"health":  "/healthz",
		},
	})
}

func (a *api) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *api) catalog(w http.ResponseWriter, _ *http.Request) {
	catalog := a.store.Catalog
	catalog.Packages = make([]Package, len(a.store.Catalog.Packages))
	for i, pkg := range a.store.Catalog.Packages {
		pkg.Available = a.store.packageAvailable(pkg)
		catalog.Packages[i] = pkg
	}
	writeJSON(w, http.StatusOK, catalog)
}

func (a *api) room(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(r.PathValue("id"))
	plan, ok := a.store.RoomPlan(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "room not found"})
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (a *api) download(w http.ResponseWriter, r *http.Request) {
	source := strings.TrimPrefix(r.URL.Path, "/files/")
	for _, pkg := range a.store.Catalog.Packages {
		if pkg.Source != source {
			continue
		}
		filename, err := a.store.packagePath(source)
		if err != nil {
			break
		}
		file, err := os.Open(filename)
		if err != nil {
			break
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			break
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
		return
	}
	http.NotFound(w, r)
}
