package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"nibrunner-cli/internal/deploy"
)

func newRunCmd() *cobra.Command {
	var (
		app         string
		port        int
		argsFlag    string
		env         map[string]string
		dataDirFlag string
		vcpu        int
		memoryMib   int
		volumeMib   int
		healthKind  string
		healthPath  string
		hostname    string
		workingDir  string
	)

	cmd := &cobra.Command{
		Use:   "run <binary>",
		Short: "Deploy a binary as an app on this host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			binaryPath := args[0]
			if _, err := os.Stat(binaryPath); err != nil {
				return fmt.Errorf("binary not found: %w", err)
			}
			if healthKind == "http" && healthPath == "" {
				return fmt.Errorf("--health-kind http needs --health-path")
			}

			var binArgs []string
			if argsFlag != "" {
				binArgs = strings.Fields(argsFlag)
			}

			result, err := deploy.Run(deploy.Options{
				BinaryPath:       binaryPath,
				App:              app,
				HTTPPort:         port,
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
	cmd.Flags().StringToStringVar(&env, "env", nil, "KEY=VALUE, repeatable; merged into the app's existing environment")
	cmd.Flags().StringVar(&workingDir, "working-dir", "/app", "the guest directory the binary runs from and writes under")
	cmd.Flags().StringVar(&dataDirFlag, "data-dir-flag", "", "e.g. --data-dir; appended with <working-dir>/data if set")
	cmd.Flags().IntVar(&vcpu, "vcpu", 1, "vCPUs given to the instance")
	cmd.Flags().IntVar(&memoryMib, "memory-mib", 256, "memory given to the instance, in MiB")
	cmd.Flags().IntVar(&volumeMib, "volume-mib", 512, "size of the app's volume, in MiB (only used the first time the app is deployed)")
	cmd.Flags().StringVar(&healthKind, "health-kind", "tcp", "http | tcp | boot-completed")
	cmd.Flags().StringVar(&healthPath, "health-path", "", "required if --health-kind is http")
	cmd.Flags().StringVar(&hostname, "hostname", "", "route the proxy to this app on this hostname")

	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("port")

	return cmd
}
