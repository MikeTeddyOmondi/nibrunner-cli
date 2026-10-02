package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// resolvedDependency is one --depends-on value, fully resolved to a local path ready to package
// as its own layer.
type resolvedDependency struct {
	path            string
	destinationName string
}

// resolveDependencies resolves every --depends-on value the same way resolveBinary resolves the
// primary one (a local path or an http(s) URL; archive extraction and --sha256 are not supported
// per-dependency in this first version, to keep the flag's syntax simple: a dependency that is
// itself a .tar.gz/.tgz/.zip fails with resolveBinary's own clear error instead of silently
// guessing a member). Returns one combined cleanup func for every dependency, so the caller only
// has one thing to defer regardless of how many were given.
func resolveDependencies(specs []string) ([]resolvedDependency, func(), error) {
	var deps []resolvedDependency
	var cleanups []func()
	cleanup := func() {
		for _, c := range cleanups {
			c()
		}
	}

	for _, spec := range specs {
		ref, overrideName := splitDependsOn(spec)
		path, depCleanup, err := resolveBinary(ref, "", "")
		cleanups = append(cleanups, depCleanup)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("--depends-on %s: %w", spec, err)
		}
		destName := overrideName
		if destName == "" {
			destName = filepath.Base(path)
		}
		deps = append(deps, resolvedDependency{path: path, destinationName: destName})
	}
	return deps, cleanup, nil
}

// splitDependsOn splits "<path-or-url>[=<name>]" on the last "=", since a dependency's path or
// URL could itself legitimately contain one earlier (a query string), but a deliberate name
// override is always appended at the end.
func splitDependsOn(spec string) (ref, overrideName string) {
	if i := strings.LastIndex(spec, "="); i != -1 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
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
