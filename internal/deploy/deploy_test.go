package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nibrunner-cli/internal/protocol"
)

// withTempHost points DesiredPath/ReportedPath/artifactDir at a temp directory for the duration
// of the test, and seeds reported.json the way an already-registered nibrunnerd would have it:
// readDesired refuses to write a document whose hostId does not match, so a deploy needs one.
func withTempHost(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oldDesired, oldReported, oldArtifacts := DesiredPath, ReportedPath, artifactDir
	DesiredPath = filepath.Join(dir, "desired.json")
	ReportedPath = filepath.Join(dir, "reported.json")
	artifactDir = filepath.Join(dir, "artifact-store")
	t.Cleanup(func() {
		DesiredPath, ReportedPath, artifactDir = oldDesired, oldReported, oldArtifacts
	})

	reported := protocol.HostReportedState{HostID: "host-local", State: "ready"}
	data, err := json.Marshal(reported)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ReportedPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFakeBinary(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readBackDesired(t *testing.T) protocol.HostDesiredState {
	t.Helper()
	data, err := os.ReadFile(DesiredPath)
	if err != nil {
		t.Fatalf("reading desired.json back: %v", err)
	}
	var desired protocol.HostDesiredState
	if err := json.Unmarshal(data, &desired); err != nil {
		t.Fatalf("desired.json does not parse: %v\n%s", err, data)
	}
	return desired
}

func TestFreshDeployWritesAWholeDocumentTheDaemonCanRead(t *testing.T) {
	dir := withTempHost(t)
	binary := writeFakeBinary(t, dir, "my-server", "pretend-binary-bytes")

	result, err := Run(Options{
		BinaryPath:    binary,
		App:           "my-app",
		HTTPPort:      8080,
		VCPUCount:     1,
		MemoryMib:     256,
		VolumeSizeMib: 512,
		Env:           map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.AppID != "my-app" {
		t.Errorf("AppID = %q, want my-app", result.AppID)
	}

	desired := readBackDesired(t)
	if desired.HostID != "host-local" {
		t.Errorf("hostId = %q, want host-local", desired.HostID)
	}
	if len(desired.Instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(desired.Instances))
	}
	instance := desired.Instances[0]
	if instance.AppID != "my-app" || instance.DesiredState != "running" {
		t.Errorf("instance = %+v", instance)
	}
	if instance.Config.HealthCheck.Kind != "tcp" {
		t.Errorf("default health check kind = %q, want tcp", instance.Config.HealthCheck.Kind)
	}
	if instance.Config.Command.Environment["FOO"] != "bar" {
		t.Errorf("environment = %v, want FOO=bar", instance.Config.Command.Environment)
	}
	if len(desired.Volumes) != 1 || desired.Volumes[0].SizeBytes != 512*1024*1024 {
		t.Errorf("volumes = %+v", desired.Volumes)
	}

	// The artifact landed under its own digest, and the bytes are exactly what was uploaded.
	artifactPath := filepath.Join(artifactDir, result.Digest)
	got, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("reading artifact back: %v", err)
	}
	if string(got) != "pretend-binary-bytes" {
		t.Errorf("artifact bytes = %q", got)
	}
}

// A nil Go map marshals to JSON null, and nibrunnerd's Rust side requires an actual map for
// environment; a null there fails the WHOLE document's parse, breaking reconciliation for every
// app on the host, not just the new one. Caught live on db9: a nibr run with no --env at all
// took down reconciliation host-wide until desired.json was hand-patched.
func TestFreshDeployWithNoEnvWritesAnEmptyMapNotNull(t *testing.T) {
	dir := withTempHost(t)
	binary := writeFakeBinary(t, dir, "my-server", "v1")

	if _, err := Run(Options{
		BinaryPath: binary, App: "my-app", HTTPPort: 8080,
		VCPUCount: 1, MemoryMib: 256, VolumeSizeMib: 512,
		// Env deliberately left nil.
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	raw, err := os.ReadFile(DesiredPath)
	if err != nil {
		t.Fatalf("reading desired.json back: %v", err)
	}
	if strings.Contains(string(raw), `"environment":null`) || strings.Contains(string(raw), `"environment": null`) {
		t.Fatalf("desired.json wrote a null environment, which nibrunnerd cannot parse:\n%s", raw)
	}

	desired := readBackDesired(t)
	if desired.Instances[0].Config.Command.Environment == nil {
		t.Error("Environment should be an empty map, not nil, once round-tripped through JSON")
	}
}

func TestARedeployKeepsOldEnvironmentAndOverlaysNewKeys(t *testing.T) {
	dir := withTempHost(t)
	binary := writeFakeBinary(t, dir, "my-server", "v1")

	if _, err := Run(Options{
		BinaryPath: binary, App: "my-app", HTTPPort: 8080,
		VCPUCount: 1, MemoryMib: 256, VolumeSizeMib: 512,
		Env: map[string]string{"SECRET": "keep-me", "LOG_LEVEL": "info"},
	}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	first := readBackDesired(t)
	firstDeployment := first.Instances[0].DeploymentID

	binary2 := writeFakeBinary(t, dir, "my-server", "v2") // different bytes, different digest
	result, err := Run(Options{
		BinaryPath: binary2, App: "my-app", HTTPPort: 8080,
		VCPUCount: 1, MemoryMib: 256, VolumeSizeMib: 512,
		Env: map[string]string{"LOG_LEVEL": "debug"}, // overlay one key, name no others
	})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	desired := readBackDesired(t)
	if len(desired.Instances) != 1 {
		t.Fatalf("a redeploy of the same app must update it in place, not add a second: got %d instances", len(desired.Instances))
	}
	if len(desired.Volumes) != 1 {
		t.Fatalf("a redeploy must not create a second volume: got %d", len(desired.Volumes))
	}
	instance := desired.Instances[0]
	if instance.DeploymentID == firstDeployment {
		t.Errorf("deploymentId did not change across a redeploy")
	}
	if instance.DeploymentID != result.DeploymentID {
		t.Errorf("returned DeploymentID does not match what was written")
	}
	env := instance.Config.Command.Environment
	if env["SECRET"] != "keep-me" {
		t.Errorf("a redeploy that names no SECRET must keep it, got %v", env)
	}
	if env["LOG_LEVEL"] != "debug" {
		t.Errorf("LOG_LEVEL should be overlaid to debug, got %v", env)
	}
}

func TestDeleteRemovesTheInstanceAndMarksItsVolumeAbsent(t *testing.T) {
	dir := withTempHost(t)
	binary := writeFakeBinary(t, dir, "my-server", "v1")

	if _, err := Run(Options{
		BinaryPath: binary, App: "my-app", HTTPPort: 8080,
		VCPUCount: 1, MemoryMib: 256, VolumeSizeMib: 512,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if err := Delete(DeleteOptions{App: "my-app"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	desired := readBackDesired(t)
	if len(desired.Instances) != 0 {
		t.Errorf("instance was not removed: %+v", desired.Instances)
	}
	if len(desired.Volumes) != 1 || desired.Volumes[0].DesiredState != "absent" {
		t.Errorf("volume should be marked absent, got %+v", desired.Volumes)
	}
}

func TestDeleteWithKeepVolumeLeavesTheVolumePresent(t *testing.T) {
	dir := withTempHost(t)
	binary := writeFakeBinary(t, dir, "my-server", "v1")

	if _, err := Run(Options{
		BinaryPath: binary, App: "my-app", HTTPPort: 8080,
		VCPUCount: 1, MemoryMib: 256, VolumeSizeMib: 512,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if err := Delete(DeleteOptions{App: "my-app", KeepVolume: true}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	desired := readBackDesired(t)
	if len(desired.Volumes) != 1 || desired.Volumes[0].DesiredState != "present" {
		t.Errorf("volume should still be present, got %+v", desired.Volumes)
	}
}

func TestDeleteOfAnUnknownAppIsAnError(t *testing.T) {
	withTempHost(t)
	if err := Delete(DeleteOptions{App: "never-deployed"}); err == nil {
		t.Fatal("expected Delete to refuse an app that was never deployed")
	}
}

func TestADeployRefusesAHostIdItDoesNotMatch(t *testing.T) {
	dir := withTempHost(t)
	binary := writeFakeBinary(t, dir, "my-server", "v1")

	// Seed a desired.json from what looks like a different host.
	foreign := protocol.HostDesiredState{
		HostID:      "some-other-host",
		Volumes:     []protocol.DesiredVolume{},
		Instances:   []protocol.DesiredInstance{},
		Checkpoints: []protocol.DesiredCheckpoint{},
		Exports:     []protocol.DesiredExport{},
	}
	data, _ := json.Marshal(foreign)
	if err := os.WriteFile(DesiredPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Run(Options{BinaryPath: binary, App: "my-app", HTTPPort: 8080, VCPUCount: 1, MemoryMib: 256, VolumeSizeMib: 512})
	if err == nil {
		t.Fatal("expected Run to refuse writing over a desired.json naming a different hostId")
	}
}
