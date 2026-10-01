// Package fetch downloads a binary (or archive containing one) from an http(s) URL, so `nibr run`
// can take a URL anywhere it already takes a local path. Everything downstream of this package
// (internal/deploy, internal/remoteapi) only ever wants a local file path; this package's whole
// job is turning a URL into one, so nothing else needs to know the difference.
package fetch

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"
)

// IsURL reports whether ref looks like something Download can fetch, rather than a local path.
// Deliberately narrow: only http/https, so a local path that happens to contain a colon (rare,
// but possible) is never misread as a URL scheme.
func IsURL(ref string) bool {
	u, err := url.Parse(ref)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// timeout bounds one download. Fixed for now rather than a flag: long enough for a large release
// binary over a slow link, short enough that a stalled connection doesn't hang `nibr run`
// forever. Revisit if a real build is ever bigger than this allows for.
const timeout = 5 * time.Minute

// Download fetches rawURL into a fresh temp file named after the URL's own path basename (so
// filepath.Base on the result is still what a human would call the file, the same property
// internal/deploy.Run already relies on for a local path), and returns that path plus a cleanup
// func the caller should defer. A non-2xx response is an error; nothing is written in that case.
func Download(rawURL string) (localPath string, cleanup func(), err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("parsing %s: %w", rawURL, err)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("fetching %s: %s", rawURL, resp.Status)
	}

	dir, err := os.MkdirTemp("", "nibr-fetch-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating a temp dir for the download: %w", err)
	}
	cleanup = func() { os.RemoveAll(dir) }

	name := path.Base(u.Path)
	if name == "" || name == "/" || name == "." {
		name = "download"
	}
	dest := filepath.Join(dir, name)

	f, err := os.Create(dest)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("creating %s: %w", dest, err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		cleanup()
		return "", nil, fmt.Errorf("downloading %s: %w", rawURL, err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("closing %s: %w", dest, err)
	}

	return dest, cleanup, nil
}
