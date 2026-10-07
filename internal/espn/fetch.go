package espn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// Timeout is the per-request HTTP timeout.
const Timeout = 10 * time.Second

// Errors returned with a nil Data by Client.Fetch. All of them mean "no data
// this time"; none should end a poll loop.
var (
	// ErrNotFound is a 404 (e.g. an unknown game). Not logged, as in the Python.
	ErrNotFound = errors.New("espn: not found")
	// ErrInvalidJSON means the body was not a JSON object. Not logged.
	ErrInvalidJSON = errors.New("espn: invalid JSON")
)

// Client fetches ESPN JSON. The zero value is ready to use.
type Client struct {
	// HTTP is the underlying client; nil means a client with Timeout.
	HTTP *http.Client
}

func (c *Client) client() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: Timeout}
}

// Fetch GETs url and decodes the JSON object. Like the Python fetch(), every
// failure means "no data": Data is nil and err says why, so callers can tell
// "fetch failed" (err != nil) from "got data" (err == nil, Data non-nil).
//   - 404 and invalid JSON return ErrNotFound / ErrInvalidJSON, unlogged.
//   - Other HTTP and network failures are logged at WARN and returned.
//   - A cancelled ctx returns ctx's error, unlogged.
func (c *Client) Fetch(ctx context.Context, url string) (Data, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, c.failed(ctx, url, "network error", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, resp.Body)
		return nil, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(io.Discard, resp.Body)
		return nil, c.failed(ctx, url, "HTTP error", fmt.Errorf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, c.failed(ctx, url, "network error", err)
	}
	data, err := Decode(body)
	if err != nil {
		return nil, ErrInvalidJSON
	}
	slog.Debug("Fetched", "url", url)
	return data, nil
}

func (c *Client) failed(ctx context.Context, url, what string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	slog.Warn(what+" fetching "+url, "err", err)
	return err
}

// ReadMock reads play-by-play from a local fixture file (--mock). Unlike
// Fetch, a missing or invalid file is an error the caller should report.
func ReadMock(path string) (Data, error) {
	slog.Debug("Reading mock file", "path", path)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := Decode(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}
