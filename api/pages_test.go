package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"time"
)

// newTestMux wires up a test HTTP mux identical to what RegisterRoutes does
// but backed by an http.ServeMux instead of the router.Router.
func newTestMux(dir string) http.Handler {
	wikiDir = dir
	pagesDir = filepath.Join(dir, "pages")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", handleHealth)
	mux.Handle("/api/v1/pages", authMiddleware(http.HandlerFunc(handlePagesList)))
	mux.Handle("/api/v1/pages/", authMiddleware(http.HandlerFunc(handlePagesDispatch)))
	return mux
}

// initGitRepo creates an initial git commit so the repo has a valid HEAD.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	// write a placeholder so the initial commit is not empty
	readmePath := filepath.Join(dir, "README.md")
	_ = os.WriteFile(readmePath, []byte("# wiki\n"), 0644)
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	_, _ = wt.Add("README.md")
	_, err = wt.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("initial commit: %v", err)
	}
}

// TestHealthNoAuth verifies that /api/v1/health is accessible without authentication.
func TestHealthNoAuth(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "pages"), 0755)
	handler := newTestMux(dir)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("health: want 200, got %d", rr.Code)
	}
}

// TestUnauthenticated verifies that missing auth returns 401.
func TestUnauthenticated(t *testing.T) {
	dir := t.TempDir()
	handler := newTestMux(dir)

	t.Setenv("BOOK_API_TOKEN", "secret")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pages", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no auth: want 401, got %d", rr.Code)
	}
}

// TestWrongToken verifies that an invalid token returns 403.
func TestWrongToken(t *testing.T) {
	dir := t.TempDir()
	handler := newTestMux(dir)

	t.Setenv("BOOK_API_TOKEN", "secret")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pages", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("wrong token: want 403, got %d", rr.Code)
	}
}

// TestNoAPIToken verifies that when BOOK_API_TOKEN is unset all API routes return 503.
func TestNoAPIToken(t *testing.T) {
	dir := t.TempDir()
	handler := newTestMux(dir)

	t.Setenv("BOOK_API_TOKEN", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pages", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("no token set: want 503, got %d", rr.Code)
	}
}

// TestCreateAndGetPage verifies POST creates a file + git commit, and GET reads it back.
func TestCreateAndGetPage(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	_ = os.MkdirAll(filepath.Join(dir, "pages"), 0755)
	handler := newTestMux(dir)

	t.Setenv("BOOK_API_TOKEN", "testtoken")
	t.Setenv("BOOK_GIT_AUTHOR", "Tester <tester@example.com>")

	// POST
	body, _ := json.Marshal(postRequest{
		Slug:     "hello",
		Markdown: "# Hello\nWorld",
		Message:  "add hello page",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pages", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer testtoken")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// verify file exists
	filePath := filepath.Join(dir, "pages", "hello.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile after POST: %v", err)
	}
	if string(data) != "# Hello\nWorld" {
		t.Errorf("file content mismatch: %q", string(data))
	}

	// verify git commit
	repo, _ := git.PlainOpen(dir)
	ref, _ := repo.Head()
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("git commit after POST: %v", err)
	}
	if commit.Message != "add hello page" {
		t.Errorf("commit message: want 'add hello page', got %q", commit.Message)
	}

	// GET
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/pages/hello", nil)
	req2.Header.Set("Authorization", "Bearer testtoken")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr2.Code)
	}
	if rr2.Body.String() != "# Hello\nWorld" {
		t.Errorf("GET body mismatch: %q", rr2.Body.String())
	}
}

// TestUpdatePage verifies PUT updates the file and creates a new commit.
func TestUpdatePage(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	_ = os.MkdirAll(filepath.Join(dir, "pages"), 0755)
	handler := newTestMux(dir)

	t.Setenv("BOOK_API_TOKEN", "testtoken")

	// create initial file
	_ = os.WriteFile(filepath.Join(dir, "pages", "edit-me.md"), []byte("old content"), 0644)
	_ = commitFile(dir, "pages/edit-me.md", "initial", false)

	// PUT
	body, _ := json.Marshal(putRequest{Markdown: "new content", Message: "update page"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pages/edit-me", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer testtoken")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	data, _ := os.ReadFile(filepath.Join(dir, "pages", "edit-me.md"))
	if string(data) != "new content" {
		t.Errorf("file after PUT: want 'new content', got %q", string(data))
	}

	repo, _ := git.PlainOpen(dir)
	ref, _ := repo.Head()
	commit, _ := repo.CommitObject(ref.Hash())
	if commit.Message != "update page" {
		t.Errorf("commit message after PUT: want 'update page', got %q", commit.Message)
	}
}

// TestDeletePage verifies DELETE removes the file and creates a commit.
func TestDeletePage(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	_ = os.MkdirAll(filepath.Join(dir, "pages"), 0755)
	handler := newTestMux(dir)

	t.Setenv("BOOK_API_TOKEN", "testtoken")

	// create file
	_ = os.WriteFile(filepath.Join(dir, "pages", "bye.md"), []byte("bye"), 0644)
	_ = commitFile(dir, "pages/bye.md", "add bye", false)

	// DELETE
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pages/bye", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE: want 200, got %d — body: %s", rr.Code, rr.Body.String())
	}

	// file should be gone
	if _, err := os.Stat(filepath.Join(dir, "pages", "bye.md")); !os.IsNotExist(err) {
		t.Error("file still exists after DELETE")
	}

	// a new commit should exist
	repo, _ := git.PlainOpen(dir)
	ref, _ := repo.Head()
	commit, _ := repo.CommitObject(ref.Hash())
	if commit.Message != "Delete bye" {
		t.Errorf("commit message after DELETE: want 'Delete bye', got %q", commit.Message)
	}
}

// TestListPages verifies GET /api/v1/pages returns correct slugs.
func TestListPages(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	pgDir := filepath.Join(dir, "pages")
	_ = os.MkdirAll(pgDir, 0755)
	handler := newTestMux(dir)

	t.Setenv("BOOK_API_TOKEN", "testtoken")

	// create some .md files
	for _, slug := range []string{"alpha", "beta", "gamma"} {
		_ = os.WriteFile(filepath.Join(pgDir, slug+".md"), []byte("# "+slug), 0644)
	}
	// a non-.md file that should be ignored
	_ = os.WriteFile(filepath.Join(pgDir, "ignore.page"), []byte("ignored"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pages", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", rr.Code)
	}

	var pages []PageInfo
	if err := json.NewDecoder(rr.Body).Decode(&pages); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	slugs := make(map[string]bool)
	for _, p := range pages {
		slugs[p.Slug] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !slugs[want] {
			t.Errorf("slug %q not in list", want)
		}
	}
	if slugs["ignore"] {
		t.Error("non-.md file 'ignore.page' appeared in list")
	}
}
