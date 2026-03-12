package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/TheBotsters/book/router"
)

// RegisterRoutes registers all API routes on the given router.
// wikiRootDir is the absolute path to the wiki's root directory.
func RegisterRoutes(r *router.Router, wikiRootDir string) {
	wikiDir = wikiRootDir
	pagesDir = filepath.Join(wikiRootDir, "pages")

	// unauthenticated
	r.HandleFunc("/api/v1/health", "API health check", handleHealth)

	// authenticated: list (GET) and create (POST) on /api/v1/pages
	r.Handle("/api/v1/pages", "API pages list/create",
		authMiddleware(http.HandlerFunc(handlePagesList)))

	// authenticated: get/put/delete on /api/v1/pages/{slug}
	r.Handle("/api/v1/pages/", "API page CRUD",
		authMiddleware(http.HandlerFunc(handlePagesDispatch)))
}

// handleHealth handles GET /api/v1/health (no auth required).
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePagesList handles GET /api/v1/pages and POST /api/v1/pages.
func handlePagesList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleListPages(w, r)
	case http.MethodPost:
		handlePostPage(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handlePagesDispatch routes /api/v1/pages/{slug} to the appropriate handler.
func handlePagesDispatch(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/pages/")
	slug = strings.TrimSuffix(slug, "/")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		handleGetPage(w, r, slug)
	case http.MethodPut:
		handlePutPage(w, r, slug)
	case http.MethodDelete:
		handleDeletePage(w, r, slug)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
