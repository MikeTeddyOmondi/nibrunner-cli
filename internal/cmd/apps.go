package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"nibrunner-cli/internal/deploy"
	"nibrunner-cli/internal/protocol"
	"nibrunner-cli/internal/remote"
)

func newAppsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "Inspect the apps this host runs",
	}
	cmd.AddCommand(newAppsListCmd())
	cmd.AddCommand(newAppsStatusCmd())
	cmd.AddCommand(newAppsLogsCmd())
	cmd.AddCommand(newAppsDeleteCmd())
	return cmd
}

func readReported() (*protocol.HostReportedState, error) {
	data, err := remote.ReadFile(deploy.ReportedPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", deploy.ReportedPath, err)
	}
	var reported protocol.HostReportedState
	if err := json.Unmarshal(data, &reported); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", deploy.ReportedPath, err)
	}
	return &reported, nil
}

func newAppsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every app this host reports",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reported, err := readReported()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(reported.Instances) == 0 {
				fmt.Fprintln(out, "no apps on this host")
				return nil
			}
			fmt.Fprintf(out, "%-20s %-12s %-10s %s\n", "APP", "STATE", "RESTARTS", "MESSAGE")
			for _, inst := range reported.Instances {
				message := ""
				if inst.Message != nil {
					message = *inst.Message
				}
				fmt.Fprintf(out, "%-20s %-12s %-10d %s\n", inst.AppID, inst.State, inst.RestartCount, message)
			}
			return nil
		},
	}
}

func newAppsStatusCmd() *cobra.Command {
	var app string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show one app's reported state in full",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reported, err := readReported()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, inst := range reported.Instances {
				if inst.AppID != app {
					continue
				}
				field(out, "appId", inst.AppID)
				field(out, "deploymentId", inst.DeploymentID)
				field(out, "state", inst.State)
				if inst.HostPort != nil {
					field(out, "hostPort", *inst.HostPort)
				} else {
					field(out, "hostPort", "(none, no slot held)")
				}
				if inst.GuestIpv4 != nil {
					field(out, "guestIpv4", *inst.GuestIpv4)
				} else {
					field(out, "guestIpv4", "(none, no slot held)")
				}
				field(out, "restartCount", inst.RestartCount)
				if inst.StartedAt != nil {
					field(out, "startedAt", *inst.StartedAt)
				}
				if inst.Message != nil {
					field(out, "message", *inst.Message)
				}
				return nil
			}
			return fmt.Errorf("no app named %q on this host", app)
		},
	}
	cmd.Flags().StringVar(&app, "app", "", "the app to show (required)")
	cmd.MarkFlagRequired("app")
	return cmd
}

func newAppsLogsCmd() *cobra.Command {
	var (
		app   string
		lines int
	)
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print an app's log tail",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := fmt.Sprintf("/var/lib/nibrunner/logs/%s.log", app)
			data, err := remote.ReadFile(path)
			if errors.Is(err, remote.ErrNotExist) {
				return fmt.Errorf("no log file yet for %q: nibrunnerd writes one once it boots the instance, which happens on its own reconcile pass after `nibr run` returns, not immediately; check `nibr apps status --app %s` and try again shortly", app, app)
			}
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}
			logLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			if len(logLines) > lines {
				logLines = logLines[len(logLines)-lines:]
			}
			out := cmd.OutOrStdout()
			for _, line := range logLines {
				fmt.Fprintln(out, line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&app, "app", "", "the app whose log to print (required)")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "how many lines from the end to print")
	cmd.MarkFlagRequired("app")
	return cmd
}

func newAppsDeleteCmd() *cobra.Command {
	var (
		app        string
		keepVolume bool
	)
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Remove an app from this host, undeploying its microVM",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := deploy.Delete(deploy.DeleteOptions{App: app, KeepVolume: keepVolume}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s from desired.json; nibrunnerd will tear it down on its next pass\n", app)
			return nil
		},
	}
	cmd.Flags().StringVar(&app, "app", "", "the app to delete (required)")
	cmd.Flags().BoolVar(&keepVolume, "keep-volume", false, "leave the app's volume in place instead of marking it absent")
	cmd.MarkFlagRequired("app")
	return cmd
}

func field(w io.Writer, name string, value any) {
	fmt.Fprintf(w, "  %-14s %v\n", name+":", value)
}
