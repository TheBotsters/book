package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// wikiDir is the root of the wiki repository (used for git operations).
var wikiDir string

// pagesDir is the directory where .md page files are stored.
var pagesDir string

// PageInfo is the JSON representation of a page in list responses.
type PageInfo struct {
	Slug       string    `json:"slug"`
	Title      string    `json:"title"`
	ModifiedAt time.Time `json:"modified_at"`
}

// validateSlug returns an error if the slug contains path traversal characters.
func validateSlug(slug string) error {
	if slug == "" {
		return errors.New("slug must not be empty")
	}
	if strings.Contains(slug, "/") || strings.Contains(slug, "..") || strings.Contains(slug, "\\") {
		return errors.New("slug must not contain '/', '..', or '\\'")
	}
	return nil
}

// pageFilePath returns the absolute path to a page file given its slug.
func pageFilePath(slug string) string {
	return filepath.Join(pagesDir, slug+".md")
}

// handleListPages handles GET /api/v1/pages
func handleListPages(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(pagesDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []PageInfo{})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read pages directory")
		return
	}

	pages := make([]PageInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		info, err := entry.Info()
		var modTime time.Time
		if err == nil {
			modTime = info.ModTime()
		}
		pages = append(pages, PageInfo{
			Slug:       slug,
			Title:      slug,
			ModifiedAt: modTime,
		})
	}

	writeJSON(w, http.StatusOK, pages)
}

// postRequest is the JSON body for POST /api/v1/pages
type postRequest struct {
	Slug     string `json:"slug"`
	Markdown string `json:"markdown"`
	Message  string `json:"message"`
}

// putRequest is the JSON body for PUT /api/v1/pages/{slug}
type putRequest struct {
	Markdown string `json:"markdown"`
	Message  string `json:"message"`
}

// handlePostPage handles POST /api/v1/pages — creates a new page.
func handlePostPage(w http.ResponseWriter, r *http.Request) {
	var req postRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := validateSlug(req.Slug); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	filePath := pageFilePath(req.Slug)

	// refuse to overwrite existing pages
	if _, err := os.Stat(filePath); err == nil {
		writeError(w, http.StatusConflict, "page already exists; use PUT to update")
		return
	}

	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create pages directory")
		return
	}

	if err := os.WriteFile(filePath, []byte(req.Markdown), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write page")
		return
	}

	msg := req.Message
	if msg == "" {
		msg = "Create " + req.Slug
	}

	// commit is best-effort; the file is already written
	_ = commitFile(wikiDir, "pages/"+req.Slug+".md", msg, false)

	writeJSON(w, http.StatusCreated, map[string]string{"slug": req.Slug})
}

// handleGetPage handles GET /api/v1/pages/{slug}
func handleGetPage(w http.ResponseWriter, r *http.Request, slug string) {
	if err := validateSlug(slug); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	data, err := os.ReadFile(pageFilePath(slug))
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "page not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read page")
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handlePutPage handles PUT /api/v1/pages/{slug} — creates or updates a page.
func handlePutPage(w http.ResponseWriter, r *http.Request, slug string) {
	if err := validateSlug(slug); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req putRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create pages directory")
		return
	}

	if err := os.WriteFile(pageFilePath(slug), []byte(req.Markdown), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write page")
		return
	}

	msg := req.Message
	if msg == "" {
		msg = "Update " + slug
	}

	_ = commitFile(wikiDir, "pages/"+slug+".md", msg, false)

	writeJSON(w, http.StatusOK, map[string]string{"slug": slug})
}

// handleDeletePage handles DELETE /api/v1/pages/{slug}
func handleDeletePage(w http.ResponseWriter, r *http.Request, slug string) {
	if err := validateSlug(slug); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	filePath := pageFilePath(slug)
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "page not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to stat page")
		return
	}

	// wt.Remove handles both file deletion and staging in go-git
	if err := commitFile(wikiDir, "pages/"+slug+".md", "Delete "+slug, true); err != nil {
		// git failed; manually remove the file so the deletion still takes effect
		_ = os.Remove(filePath)
	}

	writeJSON(w, http.StatusOK, map[string]string{"slug": slug, "deleted": "true"})
}

// commitFile opens (or lazily initializes) the git repo at wikiDir and
// commits the file at relPath. If remove is true, the file is removed from
// the worktree and staged; otherwise it is staged as added/modified.
func commitFile(wikiDir, relPath, message string, remove bool) error {
	repo, err := git.PlainOpen(wikiDir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		repo, err = git.PlainInit(wikiDir, false)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return err
	}

	if remove {
		_, err = wt.Remove(relPath)
	} else {
		_, err = wt.Add(relPath)
	}
	if err != nil {
		return err
	}

	name, email := parseAuthor(os.Getenv("BOOK_GIT_AUTHOR"))
	_, err = wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  name,
			Email: email,
			When:  time.Now(),
		},
	})
	return err
}

// parseAuthor splits "Name <email>" into name and email components.
// Falls back to "Book API <book@botsters.dev>" if the string is empty or malformed.
func parseAuthor(s string) (name, email string) {
	if s == "" {
		return "Book API", "book@botsters.dev"
	}
	if start := strings.Index(s, "<"); start != -1 {
		if end := strings.Index(s, ">"); end > start {
			name = strings.TrimSpace(s[:start])
			email = s[start+1 : end]
			return
		}
	}
	return strings.TrimSpace(s), "book@botsters.dev"
}
