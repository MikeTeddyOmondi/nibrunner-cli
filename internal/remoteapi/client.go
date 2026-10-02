// Package remoteapi is nibr's client for nibrunner-api, the optional Gin wrapper
// (github.com/MikeTeddyOmondi's nibrunner-api, sibling repo) that lets nibr's own commands run
// against a nibrunnerd host over HTTP instead of this host's local files. nibrunner-api runs
// co-located with the target nibrunnerd and performs the exact same file I/O nibr's own
// internal/deploy and internal/remote packages do locally; this package is just the HTTP side of
// that same contract.
//
// This does not change what nibrunnerd itself will accept: nibrunner-api still writes
// desired.json as a local, atomic, whole-document file write, the one thing nibrunnerd's own
// proxy code trusts. nibr talking to nibrunner-api over the network is nibr trusting
// nibrunner-api's own auth, not a new hole in nibrunnerd's boundary.
package remoteapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"nibrunner-cli/internal/protocol"
)

// Client talks to one nibrunner-api instance.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a Client. baseURL is e.g. "http://100.89.199.78:8282"; a trailing slash is
// tolerated. token is sent as a Bearer token on every request; an empty token simply sends none,
// which only works against a nibrunner-api started with AUTH_ENABLE=false.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{},
	}
}

type apiError struct {
	Error string `json:"error"`
}

// do sends the request, adds auth, and turns a non-2xx response into a Go error carrying the
// server's own {"error": "..."} message when present.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", c.baseURL, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var ae apiError
	if json.Unmarshal(body, &ae) == nil && ae.Error != "" {
		return nil, fmt.Errorf("%s: %s", resp.Status, ae.Error)
	}
	return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
}

// UploadArtifact streams path's contents to POST /v1/artifacts and returns the digest
// nibrunner-api computed from the bytes it actually received, the same way
// internal/deploy.Run's local CopyFile+verifyDigest pair would, just over HTTP instead of a
// filesystem copy.
func (c *Client) UploadArtifact(path string) (digest string, sizeBytes int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, ferr := mw.CreateFormFile("file", "upload")
		if ferr != nil {
			pw.CloseWithError(ferr)
			return
		}
		if _, cerr := io.Copy(part, f); cerr != nil {
			pw.CloseWithError(cerr)
			return
		}
		pw.CloseWithError(mw.Close())
	}()

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/artifacts", pr)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.do(req)
	if err != nil {
		return "", 0, fmt.Errorf("uploading artifact: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		Digest    string `json:"digest"`
		SizeBytes int64  `json:"sizeBytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, fmt.Errorf("decoding upload response: %w", err)
	}
	return out.Digest, out.SizeBytes, nil
}

// Dependency names one more already-uploaded artifact to package alongside the primary binary,
// at its own path, mirroring internal/deploy.Dependency but by digest rather than a local path.
type Dependency struct {
	Digest          string
	DestinationName string
}

// DeployOptions mirrors internal/deploy.Options, except it names an already-uploaded Digest
// instead of a local BinaryPath, since an HTTP deploy request carries a digest, not a path on
// this machine.
type DeployOptions struct {
	App              string
	Digest           string
	DestinationName  string
	Dependencies     []Dependency
	HTTPPort         int
	Program          string
	Args             []string
	Env              map[string]string
	WorkingDirectory string
	DataDirFlag      string
	VCPUCount        int
	MemoryMib        int
	VolumeSizeMib    int
	HealthKind       string
	HealthPath       string
	Hostname         string
}

type DeployResult struct {
	AppID        string
	DeploymentID string
	Digest       string
	Revision     string
}

// DeployApp calls POST /v1/apps, the remote equivalent of internal/deploy.Run's
// read-modify-write-desired.json cycle.
func (c *Client) DeployApp(opts DeployOptions) (*DeployResult, error) {
	dependsOn := make([]map[string]string, len(opts.Dependencies))
	for i, dep := range opts.Dependencies {
		dependsOn[i] = map[string]string{"digest": dep.Digest, "destinationName": dep.DestinationName}
	}

	body, err := json.Marshal(map[string]any{
		"app":              opts.App,
		"digest":           opts.Digest,
		"destinationName":  opts.DestinationName,
		"dependsOn":        dependsOn,
		"httpPort":         opts.HTTPPort,
		"program":          opts.Program,
		"args":             opts.Args,
		"env":              opts.Env,
		"workingDirectory": opts.WorkingDirectory,
		"dataDirFlag":      opts.DataDirFlag,
		"vcpuCount":        opts.VCPUCount,
		"memoryMib":        opts.MemoryMib,
		"volumeSizeMib":    opts.VolumeSizeMib,
		"healthKind":       opts.HealthKind,
		"healthPath":       opts.HealthPath,
		"hostname":         opts.Hostname,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/apps", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("deploying %s: %w", opts.App, err)
	}
	defer resp.Body.Close()

	var result DeployResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding deploy response: %w", err)
	}
	return &result, nil
}

// DeleteApp calls DELETE /v1/apps/:app, the remote equivalent of internal/deploy.Delete.
func (c *Client) DeleteApp(app string, keepVolume bool) error {
	u := c.baseURL + "/v1/apps/" + url.PathEscape(app)
	if keepVolume {
		u += "?keepVolume=true"
	}
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("deleting %s: %w", app, err)
	}
	defer resp.Body.Close()
	return nil
}

// SetAppState calls POST /v1/apps/:app/state, the remote equivalent of internal/deploy.SetState:
// it flips desiredState (e.g. "running" to "stopped" and back) without touching the instance or
// its volume otherwise.
func (c *Client) SetAppState(app, state string) error {
	body, err := json.Marshal(map[string]string{"state": state})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/apps/"+url.PathEscape(app)+"/state", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("setting %s's state to %q: %w", app, state, err)
	}
	defer resp.Body.Close()
	return nil
}

// AppView is an app's reported status enriched with its hostnames, mirroring nibrunner-api's own
// AppView: reported.json never carries hostnames (there's nothing for nibrunnerd to report back
// about one beyond what it was already told), so nibrunner-api cross-references desired.json and
// returns both together. local (non-remote) `nibr apps list`/`status` build the same shape by
// reading desired.json directly, see internal/cmd/apps.go.
type AppView struct {
	protocol.ReportedInstance
	Hostnames []protocol.AppHostname `json:"hostnames"`
}

// ListApps calls GET /v1/apps, the remote equivalent of reading reported.json's instances
// (enriched with hostnames from desired.json, see AppView).
func (c *Client) ListApps() ([]AppView, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/v1/apps", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("listing apps: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		Instances []AppView `json:"instances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding apps list: %w", err)
	}
	return out.Instances, nil
}

// AppStatus calls GET /v1/apps/:app, the remote equivalent of finding one instance in
// reported.json (enriched with hostnames from desired.json, see AppView).
func (c *Client) AppStatus(app string) (*AppView, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/v1/apps/"+url.PathEscape(app), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("getting status for %s: %w", app, err)
	}
	defer resp.Body.Close()

	var view AppView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		return nil, fmt.Errorf("decoding app status: %w", err)
	}
	return &view, nil
}

// HostStatus is GET /v1/host's response shape: the host-level subset of reported.json
// nibrunner-api exposes.
type HostStatus struct {
	HostID           string            `json:"hostId"`
	State            string            `json:"state"`
	ReportedAt       string            `json:"reportedAt"`
	Capacity         protocol.Capacity `json:"capacity"`
	Allocatable      protocol.Capacity `json:"allocatable"`
	AcceptedDigest   *string           `json:"acceptedDigest"`
	AcceptedRevision *string           `json:"acceptedRevision"`
	Message          *string           `json:"message"`
}

// HostStatus calls GET /v1/host.
func (c *Client) HostStatus() (*HostStatus, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/v1/host", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("getting host status: %w", err)
	}
	defer resp.Body.Close()

	var status HostStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding host status: %w", err)
	}
	return &status, nil
}

// Logs calls GET /v1/apps/:app/logs?lines=N, the remote equivalent of tailing
// logs/<app>.log directly.
func (c *Client) Logs(app string, lines int) (string, error) {
	u := c.baseURL + "/v1/apps/" + url.PathEscape(app) + "/logs"
	if lines > 0 {
		u += "?lines=" + strconv.Itoa(lines)
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("fetching logs for %s: %w", app, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
