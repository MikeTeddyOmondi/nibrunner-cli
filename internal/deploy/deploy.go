// Package deploy implements `nibr run`: the whole read-modify-write cycle that turns a local
// binary and a handful of flags into a converged nibrunner instance. It runs on the same host as
// nibrunnerd, so every step here is a local filesystem operation (no network hop, no remote API).
package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"nibrunner-cli/internal/protocol"
	"nibrunner-cli/internal/remote"
)

// Overridable so tests can point a whole deploy cycle at a temp directory instead of the real
// system paths; production code never changes these.
var (
	DesiredPath  = "/var/lib/nibrunner/desired.json"
	ReportedPath = "/var/lib/nibrunner/reported.json"
	artifactDir  = "/var/lib/nibrunner/artifact-store"
)

// Options is everything a deploy needs that the daemon cannot infer from the binary alone,
// mirroring what `nib run`'s flags ask for, adapted to running locally on the host instead of
// against a signed-in account.
type Options struct {
	BinaryPath       string
	App              string
	HTTPPort         int
	Args             []string
	Env              map[string]string
	WorkingDirectory string
	DataDirFlag      string // if set, appended as an extra arg pointing at WorkingDirectory/data
	VCPUCount        int
	MemoryMib        int
	VolumeSizeMib    int
	HealthKind       string // "http" | "tcp" | "boot-completed"
	HealthPath       string
	Hostname         string // optional platform hostname to route the proxy on
}

type Result struct {
	AppID        string
	DeploymentID string
	Digest       string
}

// Run performs one deploy: hash and copy the binary into the artifact store, then
// read-modify-write desired.json atomically so nibrunnerd's own file watcher never observes
// anything but a complete document.
func Run(opts Options) (*Result, error) {
	digest, err := sha256File(opts.BinaryPath)
	if err != nil {
		return nil, fmt.Errorf("hashing %s: %w", opts.BinaryPath, err)
	}

	objectKey := digest // content-addressed: two apps with the same bytes share a key, never collide otherwise
	artifactPath := filepath.Join(artifactDir, objectKey)
	if err := remote.CopyFile(opts.BinaryPath, artifactPath, 0644); err != nil {
		return nil, fmt.Errorf("copying into the artifact store: %w", err)
	}
	if err := verifyDigest(artifactPath, digest); err != nil {
		return nil, err
	}

	hostID, err := currentHostID()
	if err != nil {
		return nil, fmt.Errorf("reading this host's own id: %w", err)
	}

	desired, err := readDesired(hostID)
	if err != nil {
		return nil, err
	}

	volumeID := opts.App + "-vol"
	// Nanoseconds, not seconds: a deploymentId is what tells nibrunnerd a redeploy happened at
	// all (editing config under the same one is a no-op by design), so two runs within the same
	// second must not collide.
	deploymentID := fmt.Sprintf("%s-%d", opts.App, time.Now().UnixNano())
	workingDir := opts.WorkingDirectory
	if workingDir == "" {
		workingDir = "/app"
	}
	destinationPath := filepath.Join(workingDir, filepath.Base(opts.BinaryPath))

	args := append([]string{}, opts.Args...)
	if opts.DataDirFlag != "" {
		args = append(args, opts.DataDirFlag, filepath.Join(workingDir, "data"))
	}

	health := protocol.DefaultHealthCheck()
	if opts.HealthKind != "" {
		health.Kind = opts.HealthKind
	}
	if opts.HealthPath != "" {
		health.Path = opts.HealthPath
	}

	instance := protocol.DesiredInstance{
		AppID:        opts.App,
		DeploymentID: deploymentID,
		VolumeID:     volumeID,
		DesiredState: "running",
		Layers: []protocol.DesiredLayer{{
			Kind:            "executable",
			Digest:          digest,
			ObjectKey:       objectKey,
			DestinationPath: destinationPath,
		}},
		Config: protocol.AppConfig{
			HTTPPort: opts.HTTPPort,
			Command: protocol.Command{
				Program:          destinationPath,
				Args:             args,
				WorkingDirectory: workingDir,
				Environment:      opts.Env,
			},
			Resources:     protocol.Resources{VCPUCount: opts.VCPUCount, MemoryMib: opts.MemoryMib},
			HealthCheck:   health,
			RestartPolicy: protocol.DefaultRestartPolicy(),
		},
		Hostnames: []protocol.AppHostname{},
	}
	if opts.Hostname != "" {
		instance.Hostnames = []protocol.AppHostname{{Hostname: opts.Hostname, Kind: "platform"}}
	}

	// Environment is an edit, not a replacement, the same as nib: a redeploy that names no env
	// vars keeps whatever the app already had, so secrets set once are not re-typed on every
	// deploy. Everything else about the instance is replaced whole, because "everything except
	// what you named" has no sane meaning for a layer digest or a health check.
	if existing := findInstance(desired.Instances, opts.App); existing != nil {
		merged := map[string]string{}
		for k, v := range existing.Config.Command.Environment {
			merged[k] = v
		}
		for k, v := range opts.Env {
			merged[k] = v
		}
		instance.Config.Command.Environment = merged
		if len(instance.Hostnames) == 0 {
			instance.Hostnames = existing.Hostnames
		}
	}

	desired.Instances = upsertInstance(desired.Instances, instance)
	desired.Volumes = upsertVolume(desired.Volumes, protocol.DesiredVolume{
		VolumeID:     volumeID,
		AppID:        opts.App,
		SizeBytes:    int64(opts.VolumeSizeMib) * 1024 * 1024,
		DesiredState: "present",
	})

	if err := writeDesired(desired); err != nil {
		return nil, err
	}

	return &Result{AppID: opts.App, DeploymentID: deploymentID, Digest: digest}, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verifyDigest(path, want string) error {
	got, err := sha256File(path)
	if err != nil {
		return fmt.Errorf("checksumming the artifact just written: %w", err)
	}
	if got != want {
		return fmt.Errorf("artifact digest mismatch after copy: expected %s, got %s", want, got)
	}
	return nil
}

func currentHostID() (string, error) {
	data, err := remote.ReadFile(ReportedPath)
	if err != nil {
		return "", fmt.Errorf("this host has not registered with itself yet (no reported.json): %w", err)
	}
	var reported protocol.HostReportedState
	if err := json.Unmarshal(data, &reported); err != nil {
		return "", fmt.Errorf("parsing reported.json: %w", err)
	}
	if reported.HostID == "" {
		return "", errors.New("reported.json names no hostId")
	}
	return reported.HostID, nil
}

func readDesired(hostID string) (*protocol.HostDesiredState, error) {
	data, err := remote.ReadFile(DesiredPath)
	if errors.Is(err, remote.ErrNotExist) {
		return &protocol.HostDesiredState{
			Schema:      protocol.SchemaURL,
			HostID:      hostID,
			Volumes:     []protocol.DesiredVolume{},
			Instances:   []protocol.DesiredInstance{},
			Checkpoints: []protocol.DesiredCheckpoint{},
			Exports:     []protocol.DesiredExport{},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading current desired.json: %w", err)
	}
	var desired protocol.HostDesiredState
	if err := json.Unmarshal(data, &desired); err != nil {
		return nil, fmt.Errorf("parsing current desired.json: %w", err)
	}
	if desired.HostID != hostID {
		return nil, fmt.Errorf("desired.json names hostId %q but this host answers as %q; refusing to write a document it would reject", desired.HostID, hostID)
	}
	return &desired, nil
}

func writeDesired(desired *protocol.HostDesiredState) error {
	data, err := json.MarshalIndent(desired, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding desired.json: %w", err)
	}
	data = append(data, '\n')
	return remote.WriteFileAtomic(DesiredPath, data, 0644)
}

func findInstance(instances []protocol.DesiredInstance, appID string) *protocol.DesiredInstance {
	for i := range instances {
		if instances[i].AppID == appID {
			return &instances[i]
		}
	}
	return nil
}

func upsertInstance(instances []protocol.DesiredInstance, instance protocol.DesiredInstance) []protocol.DesiredInstance {
	for i := range instances {
		if instances[i].AppID == instance.AppID {
			instances[i] = instance
			return instances
		}
	}
	return append(instances, instance)
}

func upsertVolume(volumes []protocol.DesiredVolume, volume protocol.DesiredVolume) []protocol.DesiredVolume {
	for i := range volumes {
		if volumes[i].VolumeID == volume.VolumeID {
			// A volume already present keeps its size: shrinking one out from under a running
			// app's data is not something a redeploy should ever do by accident.
			return volumes
		}
	}
	return append(volumes, volume)
}
