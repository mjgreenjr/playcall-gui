package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"playcall/internal/espn"
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
	c := &logoCache{
		dir:     dir,
		client:  &http.Client{Timeout: 10 * time.Second},
		onUI:    onUI,
		waiting: map[string][]func(string){},
		failed:  map[string]bool{},
	}
	c.migrate()
	return c
}

// path is where the logo for a URL is (or will be) stored: in a folder named
// for its variant, "dark" (for dark backgrounds) or "light".
func (c *logoCache) path(logoURL, variant string) string {
	sum := sha256.Sum256([]byte(logoURL))
	return filepath.Join(c.dir, variant, hex.EncodeToString(sum[:8])+".png")
}

// migrate moves logos stored directly in the cache folder into dark/. Before
// the variants were labeled only dark ones were ever fetched, and the file
// name (a hash of the URL) is the same either way.
func (c *logoCache) migrate() {
	old, _ := filepath.Glob(filepath.Join(c.dir, "*.png"))
	if len(old) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Join(c.dir, "dark"), 0o755); err != nil {
		return
	}
	for _, f := range old {
		_ = os.Rename(f, filepath.Join(c.dir, "dark", filepath.Base(f)))
	}
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
// otherwise on the UI goroutine after it has been downloaded. variant is
// "dark" or "light" (espn.Logo.Pick says which); it names the folder the file
// is kept in. If the download fails, done is never called and the caller keeps
// its fallback.
func (c *logoCache) Get(logoURL, variant string, done func(path string)) {
	if c == nil || logoURL == "" {
		return
	}
	p := c.path(logoURL, variant)
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
	folder := filepath.Dir(dest)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(folder, "logo-*.tmp")
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

// isDark says whether the app is showing its dark theme, which decides
// which logo variant to use. Tests replace it.
var isDark = func() bool {
	return fyne.CurrentApp().Settings().ThemeVariant() == theme.VariantDark
}

// rowLogoSize is the logo size in a list row, in points.
const rowLogoSize = 28

// rowLogo is an empty logo slot for a list row.
func rowLogo() *canvas.Image {
	img := &canvas.Image{FillMode: canvas.ImageFillContain}
	img.SetMinSize(fyne.NewSize(rowLogoSize, rowLogoSize))
	return img
}

// logoSlots shows logos in the reused rows of a list. A list recycles its
// row widgets, so a download that finishes late only lands if the slot still
// wants that logo.
type logoSlots struct {
	want map[*canvas.Image]string
}

// show puts the logo for the current background (dark or light) in img once
// the cache has it; no logo empties the slot. A nil cache shows nothing.
func (l *logoSlots) show(cache *logoCache, img *canvas.Image, logo espn.Logo, dark bool) {
	url, variant := logo.Pick(dark)
	if l.want == nil {
		l.want = map[*canvas.Image]string{}
	}
	if l.want[img] == url && (url == "" || img.File != "") {
		return // already showing it; no flicker on a plain refresh
	}
	l.want[img] = url
	img.File = ""
	img.Refresh()
	cache.Get(url, variant, func(path string) {
		if l.want[img] != url {
			return
		}
		img.File = path
		img.Refresh()
	})
}
