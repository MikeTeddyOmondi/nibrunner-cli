// Package protocol mirrors nibrunner's own wire schema (desired-state.schema.json and
// reported-state.schema.json in crates/protocol/schema/), closely enough to round-trip through
// nibrunnerd without surprising it. Field shapes and names are taken directly from a running
// nibrunnerd, not guessed from the docs alone.
package protocol

// HostDesiredState is the whole of what one host should be running. It is always sent whole:
// there is no patch/diff endpoint, and none should be invented. nibrunnerd's own proxy code
// carries a comment enforcing this as a security boundary ("nothing may tell this daemon what to
// do except by writing that document"). Any tool that talks to nibrunnerd writes this file whole,
// every time.
type HostDesiredState struct {
	Schema string `json:"$schema,omitempty"`
	HostID string `json:"hostId"`
	// Required as of nibrunner v2026.9.0: 1-128 printable ASCII characters, no spaces.
	// nibrunnerd does not read it, only carries it back as reported.json's acceptedRevision;
	// a document without one is refused outright, so every write needs a value here.
	Revision    string              `json:"revision"`
	Volumes     []DesiredVolume     `json:"volumes"`
	Instances   []DesiredInstance   `json:"instances"`
	Checkpoints []DesiredCheckpoint `json:"checkpoints"`
	Exports     []DesiredExport     `json:"exports"`
}

type DesiredVolume struct {
	VolumeID     string    `json:"volumeId"`
	AppID        string    `json:"appId"`
	SizeBytes    int64     `json:"sizeBytes"`
	DesiredState string    `json:"desiredState"` // "present" | "absent"
	Seed         *Artifact `json:"seed,omitempty"`
}

type DesiredInstance struct {
	AppID        string         `json:"appId"`
	DeploymentID string         `json:"deploymentId"`
	VolumeID     string         `json:"volumeId"`
	DesiredState string         `json:"desiredState"` // "running" | "on-request" | "stopped"
	Layers       []DesiredLayer `json:"layers"`
	Config       AppConfig      `json:"config"`
	Hostnames    []AppHostname  `json:"hostnames"`
}

// DesiredLayer is a tagged union on "kind": "executable" or "filesystem". We only ever produce
// "executable": one program, packed into the guest at DestinationPath.
type DesiredLayer struct {
	Kind            string `json:"kind"`
	Digest          string `json:"digest"`
	ObjectKey       string `json:"objectKey"`
	DestinationPath string `json:"destinationPath,omitempty"` // executable only
}

type Artifact struct {
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"sizeBytes"`
	ObjectKey string `json:"objectKey"`
	Filename  string `json:"filename"`
}

type AppConfig struct {
	HTTPPort      int           `json:"httpPort"`
	Command       Command       `json:"command"`
	Resources     Resources     `json:"resources"`
	HealthCheck   HealthCheck   `json:"healthCheck"`
	RestartPolicy RestartPolicy `json:"restartPolicy"`
	Ports         []RawPort     `json:"ports,omitempty"`
}

type Command struct {
	Program          string            `json:"program"`
	Args             []string          `json:"args"`
	WorkingDirectory string            `json:"workingDirectory"`
	Environment      map[string]string `json:"environment"`
}

type Resources struct {
	VCPUCount int `json:"vcpuCount"`
	MemoryMib int `json:"memoryMib"`
}

// HealthCheck is a tagged union on "kind": "http" | "tcp" | "boot-completed". Path only applies
// to "http". boot-completed uses none of the timing fields, but we still send them; nibrunnerd
// ignores what a kind does not read.
type HealthCheck struct {
	Kind               string `json:"kind"`
	Path               string `json:"path,omitempty"`
	IntervalMs         int    `json:"intervalMs"`
	TimeoutMs          int    `json:"timeoutMs"`
	GracePeriodMs      int    `json:"gracePeriodMs"`
	HealthyThreshold   int    `json:"healthyThreshold"`
	UnhealthyThreshold int    `json:"unhealthyThreshold"`
}

type RestartPolicy struct {
	MaxRestarts      int     `json:"maxRestarts"`
	InitialBackoffMs int     `json:"initialBackoffMs"`
	MaxBackoffMs     int     `json:"maxBackoffMs"`
	BackoffFactor    float64 `json:"backoffFactor"`
	ResetAfterMs     int     `json:"resetAfterMs"`
}

type AppHostname struct {
	Hostname string `json:"hostname"`
	Kind     string `json:"kind"` // "platform" | "custom"
}

// RawPort forwards a non-HTTP port unchanged (raw-ports guide). Needs [proxy.raw] on the host;
// we do not assume it is configured.
type RawPort struct {
	Name      string `json:"name"`
	GuestPort int    `json:"guestPort"`
}

type DesiredCheckpoint struct {
	CheckpointID string `json:"checkpointId"`
	VolumeID     string `json:"volumeId"`
	DesiredState string `json:"desiredState"`
}

type DesiredExport struct {
	ExportID     string            `json:"exportId"`
	AppID        string            `json:"appId"`
	VolumeID     string            `json:"volumeId"`
	ObjectKey    string            `json:"objectKey"`
	Artifact     Artifact          `json:"artifact"`
	Environment  map[string]string `json:"environment,omitempty"`
	DesiredState string            `json:"desiredState"`
}

const SchemaURL = "https://raw.githubusercontent.com/ilbertt/nibrunner/main/crates/protocol/schema/desired.schema.json"

// DefaultHealthCheck is a sane, working-for-anything default: a TCP accept check. "http" needs a
// real path the app actually serves, which run's caller may not know; tcp only needs the app to
// be listening, which is true of literally anything we just deployed.
func DefaultHealthCheck() HealthCheck {
	return HealthCheck{
		Kind:               "tcp",
		IntervalMs:         5000,
		TimeoutMs:          2000,
		GracePeriodMs:      15000,
		HealthyThreshold:   2,
		UnhealthyThreshold: 3,
	}
}

func DefaultRestartPolicy() RestartPolicy {
	return RestartPolicy{
		MaxRestarts:      5,
		InitialBackoffMs: 1000,
		MaxBackoffMs:     30000,
		BackoffFactor:    2,
		ResetAfterMs:     300000,
	}
}
