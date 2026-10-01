package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"nibrunner-cli/internal/archive"
	"nibrunner-cli/internal/fetch"
)

// resolveBinary turns ref, `nibr run`'s positional argument, into a local file path ready to
// deploy, plus a cleanup func the caller should always defer (even on error: a partial download
// or extraction may still need removing). ref may be a local path (the only thing this ever
// supported, and still the only case that touches no network) or an http(s) URL; either may in
// turn be a .tar.gz/.tgz/.zip/.gz, in which case archiveMember names the file inside it to
// deploy (required for .tar.gz/.tgz/.zip, which can hold more than one file; ignored for a plain
// .gz, which holds exactly one). If sha256sum is non-empty, the final file's digest must match it
// exactly or this fails closed.
//
// Everything after this call (internal/deploy.Run, runRemote) only ever sees a local path, the
// same thing `nibr run ./my-server` always gave them: a URL or an archive is resolved down to
// that before either one is ever reached.
func resolveBinary(ref, archiveMember, sha256sum string) (path string, cleanup func(), err error) {
	cleanup = func() {}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()

	var source string
	if fetch.IsURL(ref) {
		downloaded, dlCleanup, derr := fetch.Download(ref)
		if derr != nil {
			return "", cleanup, derr
		}
		cleanup = dlCleanup
		source = downloaded
	} else {
		if _, serr := os.Stat(ref); serr != nil {
			return "", cleanup, fmt.Errorf("binary not found: %w", serr)
		}
		source = ref
	}

	switch {
	case archive.RequiresMember(source) && archiveMember == "":
		return "", cleanup, fmt.Errorf("%s looks like an archive: pass --archive-member <path-inside-it> naming the binary to deploy", source)
	case archiveMember != "" && !archive.IsArchive(source):
		return "", cleanup, fmt.Errorf("--archive-member was given but %s is not a recognized archive (.tar.gz, .tgz, .zip, .gz)", source)
	case archive.IsArchive(source):
		extracted, exCleanup, eerr := archive.Extract(source, archiveMember)
		if eerr != nil {
			return "", cleanup, eerr
		}
		downloadCleanup := cleanup
		cleanup = func() { exCleanup(); downloadCleanup() }
		source = extracted
	}

	if sha256sum != "" {
		if verr := verifySHA256(source, sha256sum); verr != nil {
			return "", cleanup, verr
		}
	}

	return source, cleanup, nil
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 mismatch for %s: expected %s, got %s", path, want, got)
	}
	return nil
}
