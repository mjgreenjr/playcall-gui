package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// png is a valid enough payload: the cache only checks the content type.
var pngBytes = []byte("\x89PNG\r\n\x1a\nfake")

func logoServer(t *testing.T, hits *atomic.Int32, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if status != http.StatusOK {
			http.Error(w, "no", status)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func inline(f func()) { f() }

func TestLogoCacheDownloadsOnceAndReuses(t *testing.T) {
	var hits atomic.Int32
	srv := logoServer(t, &hits, http.StatusOK)
	c := newLogoCache(t.TempDir(), inline)
	var got []string
	c.Get(srv.URL+"/a.png", "dark", func(p string) { got = append(got, p) })
	if len(got) != 1 {
		t.Fatalf("callbacks: %v", got)
	}
	b, err := os.ReadFile(got[0])
	if err != nil || string(b) != string(pngBytes) {
		t.Fatalf("file %q: %v", b, err)
	}
	// Second request: from disk, no new download.
	c.Get(srv.URL+"/a.png", "dark", func(p string) { got = append(got, p) })
	if len(got) != 2 || got[1] != got[0] || hits.Load() != 1 {
		t.Errorf("got %v after %d download(s)", got, hits.Load())
	}
	// A new cache on the same folder (the next run) also finds it.
	c2 := newLogoCache(c.dir, inline)
	c2.Get(srv.URL+"/a.png", "dark", func(string) {})
	if hits.Load() != 1 {
		t.Errorf("downloaded again after a restart: %d", hits.Load())
	}
}

func TestLogoCacheFailureIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := logoServer(t, &hits, http.StatusNotFound)
	c := newLogoCache(t.TempDir(), inline)
	called := false
	for i := 0; i < 3; i++ {
		c.Get(srv.URL+"/missing.png", "dark", func(string) { called = true })
	}
	if called || hits.Load() != 1 {
		t.Errorf("called %v after %d request(s)", called, hits.Load())
	}
	if entries, _ := os.ReadDir(c.dir); len(entries) != 0 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestLogoCacheIgnoresNonImages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>"))
	}))
	defer srv.Close()
	c := newLogoCache(t.TempDir(), inline)
	called := false
	c.Get(srv.URL+"/x", "dark", func(string) { called = true })
	if called {
		t.Error("an HTML page was cached as a logo")
	}
}

func TestLogoCacheSharesOneDownloadBetweenWaiters(t *testing.T) {
	var hits atomic.Int32
	srv := logoServer(t, &hits, http.StatusOK)
	c := newLogoCache(t.TempDir(), inline)
	// Hold the download back so both requests wait on it.
	release := make(chan struct{})
	started := make(chan struct{})
	old := runAsync
	runAsync = func(f func()) { go func() { close(started); <-release; f() }() }
	defer func() { runAsync = old }()

	done := make(chan string, 2)
	c.Get(srv.URL+"/b.png", "dark", func(p string) { done <- p })
	<-started
	c.Get(srv.URL+"/b.png", "dark", func(p string) { done <- p })
	close(release)
	a, b := <-done, <-done
	if a != b || hits.Load() != 1 {
		t.Errorf("paths %q %q, %d download(s)", a, b, hits.Load())
	}
}

func TestLogoCacheNilAndEmptyAreNoOps(t *testing.T) {
	var c *logoCache
	c.Get("https://x/y.png", "dark", func(string) { t.Error("nil cache called back") })
	newLogoCache(t.TempDir(), inline).Get("", "dark", func(string) { t.Error("empty URL called back") })
}

func TestSizedURL(t *testing.T) {
	got := sizedURL("https://a.espncdn.com/i/teamlogos/nfl/500-dark/dal.png")
	want := "https://a.espncdn.com/combiner/i?img=/i/teamlogos/nfl/500-dark/dal.png&w=96&h=96"
	if got != want {
		t.Errorf("got %q", got)
	}
	if other := "https://example.com/i/logo.png"; sizedURL(other) != other {
		t.Errorf("other hosts must be left alone: %q", sizedURL(other))
	}
	if guid := "https://a.espncdn.com/guid/abc/logos/x.png"; sizedURL(guid) != guid {
		t.Errorf("non-/i/ paths must be left alone: %q", sizedURL(guid))
	}
}

func TestLogoCacheKeepsVariantsInSeparateFolders(t *testing.T) {
	var hits atomic.Int32
	srv := logoServer(t, &hits, http.StatusOK)
	c := newLogoCache(t.TempDir(), inline)
	var dark, light string
	c.Get(srv.URL+"/d.png", "dark", func(p string) { dark = p })
	c.Get(srv.URL+"/l.png", "light", func(p string) { light = p })
	if filepath.Dir(dark) != filepath.Join(c.dir, "dark") || filepath.Dir(light) != filepath.Join(c.dir, "light") {
		t.Errorf("dark %q, light %q", dark, light)
	}
	for _, p := range []string{dark, light} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
}

func TestLogoCacheMovesOldFlatFilesIntoDark(t *testing.T) {
	var hits atomic.Int32
	srv := logoServer(t, &hits, http.StatusOK)
	dir := t.TempDir()
	// A file from before variants were labeled: flat, named by the URL's hash.
	probe := newLogoCache(t.TempDir(), inline)
	url := srv.URL + "/old.png"
	flat := filepath.Join(dir, filepath.Base(probe.path(url, "dark")))
	if err := os.WriteFile(flat, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	c := newLogoCache(dir, inline)
	if _, err := os.Stat(flat); err == nil {
		t.Error("the flat file was left behind")
	}
	var got string
	c.Get(url, "dark", func(p string) { got = p })
	if got != c.path(url, "dark") || hits.Load() != 0 {
		t.Errorf("got %q after %d download(s); the migrated file should be used", got, hits.Load())
	}
}

func TestLogoCacheMigrationIsHarmlessWithNoFolderYet(t *testing.T) {
	c := newLogoCache(filepath.Join(t.TempDir(), "not", "there", "yet"), inline)
	c.migrate() // must not create or fail on anything
}
