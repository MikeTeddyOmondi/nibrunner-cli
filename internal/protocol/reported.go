package protocol

// HostReportedState is the whole of what one host says it is running, as nibrunnerd wrote it
// after its last reconcile pass. It is the only thing that answers "did my write land, and why
// not if it didn't", since nibrunnerd's proxy is read-only by design, so this file is the one place
// status comes from.
type HostReportedState struct {
	HostID      string               `json:"hostId"`
	ReportedAt  string               `json:"reportedAt"`
	State       string               `json:"state"`
	Capacity    Capacity             `json:"capacity"`
	Allocatable Capacity             `json:"allocatable"`
	Versions    Versions             `json:"versions"`
	Volumes     []ReportedVolume     `json:"volumes"`
	Instances   []ReportedInstance   `json:"instances"`
	Checkpoints []ReportedCheckpoint `json:"checkpoints"`
	Exports     []ReportedExport     `json:"exports"`
	Message     *string              `json:"message"`
}

type Capacity struct {
	VCPUCount  int   `json:"vcpuCount"`
	MemoryMib  int   `json:"memoryMib"`
	CacheBytes int64 `json:"cacheBytes"`
}

type Versions struct {
	Agent       string `json:"agent"`
	GuestImage  string `json:"guestImage"`
	ZeroFS      string `json:"zerofs"`
	Firecracker string `json:"firecracker"`
}

type ReportedVolume struct {
	VolumeID      string  `json:"volumeId"`
	AppID         string  `json:"appId"`
	State         string  `json:"state"`
	SizeBytes     int64   `json:"sizeBytes"`
	DevicePath    *string `json:"devicePath,omitempty"`
	Message       *string `json:"message,omitempty"`
	StoragePrefix *string `json:"storagePrefix,omitempty"`
}

// ReportedInstance's HostPort and GuestIpv4 are both optional: absent for a record whose app
// never held a network slot: a start refused before one existed, such as the host being laid
// out for fewer apps than it is asked to run (nibrunner's own fix/report-first-start-failure).
type ReportedInstance struct {
	AppID         string           `json:"appId"`
	DeploymentID  string           `json:"deploymentId"`
	State         string           `json:"state"`
	HostPort      *int             `json:"hostPort,omitempty"`
	GuestIpv4     *string          `json:"guestIpv4,omitempty"`
	LayerDigests  []string         `json:"layerDigests"`
	RestartCount  int              `json:"restartCount"`
	LastRestart   *ReportedRestart `json:"lastRestart,omitempty"`
	StartedAt     *string          `json:"startedAt,omitempty"`
	LastHealthyAt *string          `json:"lastHealthyAt,omitempty"`
	ConvergedAt   *string          `json:"convergedAt,omitempty"`
	LastExitCode  *int             `json:"lastExitCode,omitempty"`
	Message       *string          `json:"message,omitempty"`
}

type ReportedRestart struct {
	At        string `json:"at"`
	Attempt   int    `json:"attempt"`
	Budget    int    `json:"budget"`
	Reason    string `json:"reason"`
	BackoffMs int    `json:"backoffMs"`
}

type ReportedCheckpoint struct {
	CheckpointID string  `json:"checkpointId"`
	State        string  `json:"state"`
	Message      *string `json:"message,omitempty"`
}

type ReportedExport struct {
	ExportID string  `json:"exportId"`
	State    string  `json:"state"`
	Message  *string `json:"message,omitempty"`
}
