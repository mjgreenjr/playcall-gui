package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// logoSize is the pixel size logos are fetched at. ESPN's originals are 500px
// (30-40 KB); at this size they are about 4 KB and sharp enough for a header.
const logoSize = 96

// logoCache keeps team logos on disk, one file per URL, and downloads the
// missing ones in the background. A logo is fetched at most once per URL, and
// one that fails is not retried until the next run.
type logoCache struct {
	dir    string
	client *http.Client
	// onUI runs a callback on the UI goroutine (fyne.Do); tests run it inline.
	onUI func(func())

	mu      sync.Mutex
	waiting map[string][]func(path string) // callbacks per URL being downloaded
	failed  map[string]bool
}

func newLogoCache(dir string, onUI func(func())) *logoCache {
	return &logoCache{
		dir:     dir,
		client:  &http.Client{Timeout: 10 * time.Second},
		onUI:    onUI,
		waiting: map[string][]func(string){},
		failed:  map[string]bool{},
	}
}

// path is where the logo for a URL is (or will be) stored.
func (c *logoCache) path(logoURL string) string {
	sum := sha256.Sum256([]byte(logoURL))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:8])+".png")
}

// sizedURL asks ESPN's image service for a small copy of one of its logos;
// any other URL is fetched as it is.
func sizedURL(logoURL string) string {
	u, err := url.Parse(logoURL)
	if err != nil || u.Host != "a.espncdn.com" || !strings.HasPrefix(u.Path, "/i/") {
		return logoURL
	}
	return fmt.Sprintf("https://a.espncdn.com/combiner/i?img=%s&w=%d&h=%d", u.Path, logoSize, logoSize)
}

// Get calls done with the logo's file path: at once if it is already on disk,
// otherwise on the UI goroutine after it has been downloaded. If the download
// fails, done is never called and the caller keeps its fallback.
func (c *logoCache) Get(logoURL string, done func(path string)) {
	if c == nil || logoURL == "" {
		return
	}
	p := c.path(logoURL)
	if _, err := os.Stat(p); err == nil {
		done(p)
		return
	}
	c.mu.Lock()
	if c.failed[logoURL] {
		c.mu.Unlock()
		return
	}
	starting := len(c.waiting[logoURL]) == 0
	c.waiting[logoURL] = append(c.waiting[logoURL], done)
	c.mu.Unlock()
	if !starting {
		return
	}
	runAsync(func() {
		err := c.download(logoURL, p)
		c.mu.Lock()
		callbacks := c.waiting[logoURL]
		delete(c.waiting, logoURL)
		if err != nil {
			c.failed[logoURL] = true
		}
		c.mu.Unlock()
		if err != nil {
			slog.Debug("Could not fetch logo", "url", logoURL, "err", err)
			return
		}
		for _, cb := range callbacks {
			c.onUI(func() { cb(p) })
		}
	})
}

// download fetches the logo into a temporary file and renames it into place,
// so a half-written file is never mistaken for a logo.
func (c *logoCache) download(logoURL, dest string) error {
	resp, err := c.client.Get(sizedURL(logoURL))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("not an image: %q", ct)
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, "logo-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 2<<20)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}
