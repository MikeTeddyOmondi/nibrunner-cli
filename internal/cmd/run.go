package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"nibrunner-cli/internal/deploy"
	"nibrunner-cli/internal/remoteapi"
)

func newRunCmd() *cobra.Command {
	var (
		app           string
		port          int
		argsFlag      string
		argTokens     []string
		program       string
		env           map[string]string
		dataDirFlag   string
		vcpu          int
		memoryMib     int
		volumeMib     int
		healthKind    string
		healthPath    string
		hostname      string
		workingDir    string
		archiveMember string
		sha256sum     string
	)

	cmd := &cobra.Command{
		Use:   "run <binary>",
		Short: "Deploy a binary as an app on this host",
		Long: `Deploy a binary as an app on this host.

<binary> is a local path, as always, or now also an http(s) URL (e.g. a GitHub release asset).
If what that resolves to is a .tar.gz, .tgz or .zip, --archive-member names which file inside it
to deploy; a plain .gz is unwrapped automatically since it can only ever hold one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			binaryPath, cleanup, err := resolveBinary(args[0], archiveMember, sha256sum)
			defer cleanup()
			if err != nil {
				return err
			}
			if healthKind == "http" && healthPath == "" {
				return fmt.Errorf("--health-kind http needs --health-path")
			}

			// --args is space-split, for simple cases; --arg is repeatable and takes each
			// token exactly as given, for anything with quoting or embedded spaces (e.g. a
			// `/bin/sh -c "a; exec b"` wrapper, where --program overrides the binary itself).
			var binArgs []string
			if argsFlag != "" {
				binArgs = strings.Fields(argsFlag)
			}
			binArgs = append(binArgs, argTokens...)

			if rc := remoteClient(); rc != nil {
				return runRemote(cmd, rc, binaryPath, deployArgs{
					app, port, program, binArgs, env, workingDir, dataDirFlag,
					vcpu, memoryMib, volumeMib, healthKind, healthPath, hostname,
				})
			}

			result, err := deploy.Run(deploy.Options{
				BinaryPath:       binaryPath,
				App:              app,
				HTTPPort:         port,
				Program:          program,
				Args:             binArgs,
				Env:              env,
				WorkingDirectory: workingDir,
				DataDirFlag:      dataDirFlag,
				VCPUCount:        vcpu,
				MemoryMib:        memoryMib,
				VolumeSizeMib:    volumeMib,
				HealthKind:       healthKind,
				HealthPath:       healthPath,
				Hostname:         hostname,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deployed %s\n  deploymentId: %s\n  digest:       %s\n", result.AppID, result.DeploymentID, result.Digest)
			fmt.Fprintf(cmd.OutOrStdout(), "watch it converge with: nibr apps status --app %s\n", result.AppID)
			return nil
		},
	}

	cmd.Flags().StringVar(&app, "app", "", "the app's identifier (required)")
	cmd.Flags().IntVar(&port, "port", 0, "the guest port the binary listens on (required)")
	cmd.Flags().StringVar(&argsFlag, "args", "", `arguments passed to the binary, e.g. --args "serve --verbose"`)
	cmd.Flags().StringArrayVar(&argTokens, "arg", nil, `one exact argument, repeatable; use for anything --args's space-split would mangle, e.g. --arg -c --arg "a && exec b"`)
	cmd.Flags().StringVar(&program, "program", "", "override the program run instead of the deployed binary itself, e.g. /bin/sh, with the binary still deployed and reachable under --working-dir")
	cmd.Flags().StringToStringVar(&env, "env", nil, "KEY=VALUE, repeatable; merged into the app's existing environment")
	cmd.Flags().StringVar(&workingDir, "working-dir", "/app", "the guest directory the binary runs from and writes under")
	cmd.Flags().StringVar(&dataDirFlag, "data-dir-flag", "", "e.g. --data-dir; appended with <working-dir>/data if set")
	cmd.Flags().IntVar(&vcpu, "vcpu", 1, "vCPUs given to the instance")
	cmd.Flags().IntVar(&memoryMib, "memory-mib", 256, "memory given to the instance, in MiB")
	cmd.Flags().IntVar(&volumeMib, "volume-mib", 512, "size of the app's volume, in MiB (only used the first time the app is deployed)")
	cmd.Flags().StringVar(&healthKind, "health-kind", "tcp", "http | tcp | boot-completed")
	cmd.Flags().StringVar(&healthPath, "health-path", "", "required if --health-kind is http")
	cmd.Flags().StringVar(&hostname, "hostname", "", "route the proxy to this app on this hostname")
	cmd.Flags().StringVar(&archiveMember, "archive-member", "", "path (or unambiguous basename) of the binary inside <binary>, when that's a .tar.gz, .tgz or .zip")
	cmd.Flags().StringVar(&sha256sum, "sha256", "", "expected sha256 of the resolved binary; refuses to deploy on a mismatch")

	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("port")

	return cmd
}

// deployArgs bundles newRunCmd's flags for runRemote, since a cobra RunE closure's local flag
// variables don't otherwise have a convenient single value to hand off.
type deployArgs struct {
	app         string
	port        int
	program     string
	binArgs     []string
	env         map[string]string
	workingDir  string
	dataDirFlag string
	vcpu        int
	memoryMib   int
	volumeMib   int
	healthKind  string
	healthPath  string
	hostname    string
}

// runRemote is --remote's path through `nibr run`: upload the binary to nibrunner-api, then
// deploy by digest. It mirrors internal/deploy.Run's own two steps (copy into the artifact
// store, then read-modify-write desired.json) except the first step is an HTTP upload and the
// second an HTTP POST, both performed by nibrunner-api on the target host rather than by this
// process on local files.
func runRemote(cmd *cobra.Command, rc *remoteapi.Client, binaryPath string, a deployArgs) error {
	digest, _, err := rc.UploadArtifact(binaryPath)
	if err != nil {
		return err
	}

	result, err := rc.DeployApp(remoteapi.DeployOptions{
		App:    a.app,
		Digest: digest,
		// Matches internal/deploy.Run's own default: the binary's own basename inside the
		// guest, not the app name, so --remote and local `nibr run` behave the same way.
		DestinationName:  filepath.Base(binaryPath),
		HTTPPort:         a.port,
		Program:          a.program,
		Args:             a.binArgs,
		Env:              a.env,
		WorkingDirectory: a.workingDir,
		DataDirFlag:      a.dataDirFlag,
		VCPUCount:        a.vcpu,
		MemoryMib:        a.memoryMib,
		VolumeSizeMib:    a.volumeMib,
		HealthKind:       a.healthKind,
		HealthPath:       a.healthPath,
		Hostname:         a.hostname,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "deployed %s\n  deploymentId: %s\n  digest:       %s\n", result.AppID, result.DeploymentID, result.Digest)
	fmt.Fprintf(cmd.OutOrStdout(), "watch it converge with: nibr apps status --app %s --remote %s\n", result.AppID, remoteURL)
	return nil
}
