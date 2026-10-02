// Package remote talks to nibrunnerd the only way its own proxy code insists on: a local write to
// desired.json, atomically. nibr runs on the same host as nibrunnerd, with no network hop and no
// SSH/scp, so "remote" here just means "not in this process's own memory."
//
// Nothing here adds a network-reachable write path. nibrunnerd's proxy code carries a comment
// defending exactly that boundary: "nothing may tell this daemon what to do except by writing
// that document." A local CLI writing a local file, as the operator who is already logged into
// this box, is the boundary nibrunnerd was built to trust; a remote API would not be.
package remote

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ErrNotExist = os.ErrNotExist

// ReadFile reads one file's whole contents. A missing file returns ErrNotExist unwrapped-comparable
// via errors.Is, so callers that treat "nothing here yet" as the ordinary state of a fresh host,
// not a failure, can tell the two apart.
func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotExist
	}
	return data, err
}

// WriteFileAtomic writes data to path as a whole, or not at all: it lands in a sibling temp file
// on the same filesystem first (so the rename is atomic, not a cross-device copy), then one
// rename puts it in place. nibrunnerd's file watcher can only ever observe the finished file this
// way, never a partial write mid-read.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file beside %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("setting mode on %s: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("flushing %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, path, err)
	}
	return nil
}

// CopyFile copies src to dst byte-for-byte, the artifact store's own way of receiving a binary:
// `install -D -m 0644 ./my-server /var/lib/nibrunner/artifact-store/my-server` in the docs, done
// in Go so the digest can be verified against the same bytes that were just written.
//
// If src and dst already name the same file (same device and inode, however they got there: a
// symlink, a bind mount, or a caller naming an artifact already in the store by its own store
// path), this is a deliberate no-op rather than a copy. Opening dst with O_TRUNC before reading
// src would otherwise truncate the one underlying file both names point at, destroying the very
// bytes the copy was asked to preserve.
func CopyFile(src, dst string, mode os.FileMode) error {
	if same, err := sameFile(src, dst); err != nil {
		return err
	} else if same {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copying to %s: %w", dst, err)
	}
	return out.Close()
}

// sameFile reports whether src and dst resolve to the same underlying file. A missing src or dst
// is "not the same file" here, not an error: CopyFile's own os.Open(src) is what should surface a
// missing source, with its own clearer error message.
func sameFile(src, dst string) (bool, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, nil
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return false, nil
	}
	return os.SameFile(srcInfo, dstInfo), nil
}
