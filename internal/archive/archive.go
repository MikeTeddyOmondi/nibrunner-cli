// Package archive pulls one binary out of a .tar.gz/.tgz/.zip/.gz, so `nibr run` can take a
// GitHub release asset (almost always one of these) directly, rather than requiring the operator
// to unpack it by hand first. As with internal/fetch, downstream code only ever sees a resulting
// local file path; this package's job ends there.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IsArchive reports whether path's extension is one this package knows how to open at all.
func IsArchive(path string) bool {
	return hasAnySuffix(path, ".tar.gz", ".tgz", ".zip", ".gz")
}

// RequiresMember reports whether Extract needs a non-empty member for path: true for .tar.gz,
// .tgz and .zip, which can hold more than one file, false for a plain .gz, which by construction
// holds exactly one.
func RequiresMember(path string) bool {
	return hasAnySuffix(path, ".tar.gz", ".tgz", ".zip")
}

func hasAnySuffix(path string, suffixes ...string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}

// Extract pulls member out of path into a fresh temp file named after member's own basename (so
// filepath.Base on the result is the executable's real name, not the archive's), and returns that
// path plus a cleanup func the caller should defer. member is ignored for a plain .gz. member may
// be either an entry's exact path inside the archive, or just its basename, as long as that's
// unambiguous.
func Extract(path, member string) (extractedPath string, cleanup func(), err error) {
	switch {
	case hasAnySuffix(path, ".tar.gz", ".tgz"):
		return extractTarGz(path, member)
	case strings.HasSuffix(path, ".zip"):
		return extractZip(path, member)
	case strings.HasSuffix(path, ".gz"):
		return extractGzip(path)
	default:
		return "", nil, fmt.Errorf("don't know how to extract %s: supported archive extensions are .tar.gz, .tgz, .zip, .gz", path)
	}
}

// extractTarGz needs two passes over the stream: tar+gzip is forward-only, so which entry to
// extract (an exact name match, or the one entry whose basename matches) has to be decided before
// any extraction starts, not discovered partway through one.
func extractTarGz(path, member string) (string, func(), error) {
	chosen, err := resolveTarGzEntry(path, member)
	if err != nil {
		return "", nil, err
	}

	tr, closer, err := openTarGz(path)
	if err != nil {
		return "", nil, err
	}
	defer closer()
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("reading %s as tar: %w", path, err)
		}
		if hdr.Name == chosen {
			return writeTemp(tr, member)
		}
	}
	return "", nil, fmt.Errorf("entry %q vanished from %s between passes", chosen, path)
}

// resolveTarGzEntry scans every header (without extracting anything) to decide which single
// entry member names: an exact path match wins outright; otherwise exactly one basename match is
// required, since more than one would be an arbitrary, silent choice.
func resolveTarGzEntry(path, member string) (string, error) {
	tr, closer, err := openTarGz(path)
	if err != nil {
		return "", err
	}
	defer closer()

	var basenameMatches []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("reading %s as tar: %w", path, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if hdr.Name == member {
			return hdr.Name, nil
		}
		if filepath.Base(hdr.Name) == member {
			basenameMatches = append(basenameMatches, hdr.Name)
		}
	}
	switch len(basenameMatches) {
	case 0:
		return "", fmt.Errorf("no entry named %q (or with that basename) in %s", member, path)
	case 1:
		return basenameMatches[0], nil
	default:
		return "", fmt.Errorf("%q is ambiguous in %s, matches: %s (use one of these full paths as --archive-member instead)",
			member, path, strings.Join(basenameMatches, ", "))
	}
}

func openTarGz(path string) (*tar.Reader, func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", path, err)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("reading %s as gzip: %w", path, err)
	}
	return tar.NewReader(gz), func() error {
		gz.Close()
		return f.Close()
	}, nil
}

// extractZip has random access to the central directory, so unlike tar.gz it needs only one pass.
func extractZip(path, member string) (string, func(), error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", nil, fmt.Errorf("opening %s as zip: %w", path, err)
	}
	defer r.Close()

	var exact *zip.File
	var basenameMatches []*zip.File
	for _, f := range r.File {
		if f.Name == member {
			exact = f
			break
		}
		if filepath.Base(f.Name) == member {
			basenameMatches = append(basenameMatches, f)
		}
	}

	var found *zip.File
	switch {
	case exact != nil:
		found = exact
	case len(basenameMatches) == 1:
		found = basenameMatches[0]
	case len(basenameMatches) > 1:
		names := make([]string, len(basenameMatches))
		for i, f := range basenameMatches {
			names[i] = f.Name
		}
		return "", nil, fmt.Errorf("%q is ambiguous in %s, matches: %s (use one of these full paths as --archive-member instead)",
			member, path, strings.Join(names, ", "))
	default:
		return "", nil, fmt.Errorf("no entry named %q (or with that basename) in %s", member, path)
	}

	rc, err := found.Open()
	if err != nil {
		return "", nil, fmt.Errorf("reading %s from %s: %w", found.Name, path, err)
	}
	defer rc.Close()

	return writeTemp(rc, member)
}

func extractGzip(path string) (string, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", nil, fmt.Errorf("reading %s as gzip: %w", path, err)
	}
	defer gz.Close()

	name := strings.TrimSuffix(filepath.Base(path), ".gz")
	return writeTemp(gz, name)
}

// writeTemp copies r into a fresh temp file named after name's own basename, mode 0755 since the
// whole point of extracting is to deploy it as an executable.
func writeTemp(r io.Reader, name string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "nibr-extract-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating a temp dir to extract into: %w", err)
	}
	cleanup := func() { os.RemoveAll(dir) }

	dest := filepath.Join(dir, filepath.Base(name))
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("creating %s: %w", dest, err)
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		cleanup()
		return "", nil, fmt.Errorf("extracting to %s: %w", dest, err)
	}
	if err := out.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("closing %s: %w", dest, err)
	}
	return dest, cleanup, nil
}
