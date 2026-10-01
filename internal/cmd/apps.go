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
	"nibrunner-cli/internal/remoteapi"
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

// readDesiredHostnames maps every app currently in desired.json to its hostnames, the local
// equivalent of what nibrunner-api's own hostnameIndex does for --remote mode: reported.json
// never carries hostnames, so local mode cross-references desired.json the same way. Best-effort,
// nil on any error, so a local desired.json hiccup still lets reported status print, just without
// hostnames.
func readDesiredHostnames() map[string][]protocol.AppHostname {
	data, err := remote.ReadFile(deploy.DesiredPath)
	if err != nil {
		return nil
	}
	var desired protocol.HostDesiredState
	if err := json.Unmarshal(data, &desired); err != nil {
		return nil
	}
	index := make(map[string][]protocol.AppHostname, len(desired.Instances))
	for _, inst := range desired.Instances {
		index[inst.AppID] = inst.Hostnames
	}
	return index
}

func hostnameList(hostnames []protocol.AppHostname) string {
	if len(hostnames) == 0 {
		return "-"
	}
	names := make([]string, len(hostnames))
	for i, h := range hostnames {
		names[i] = h.Hostname
	}
	return strings.Join(names, ",")
}

func newAppsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every app this host reports",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var views []remoteapi.AppView
			if rc := remoteClient(); rc != nil {
				var err error
				views, err = rc.ListApps()
				if err != nil {
					return err
				}
			} else {
				reported, err := readReported()
				if err != nil {
					return err
				}
				hostnames := readDesiredHostnames()
				views = make([]remoteapi.AppView, len(reported.Instances))
				for i, inst := range reported.Instances {
					views[i] = remoteapi.AppView{ReportedInstance: inst, Hostnames: hostnames[inst.AppID]}
				}
			}
			printAppsList(cmd.OutOrStdout(), views)
			return nil
		},
	}
}

func printAppsList(out io.Writer, views []remoteapi.AppView) {
	if len(views) == 0 {
		fmt.Fprintln(out, "no apps on this host")
		return
	}
	fmt.Fprintf(out, "%-20s %-12s %-10s %-30s %s\n", "APP", "STATE", "RESTARTS", "HOSTNAMES", "MESSAGE")
	for _, v := range views {
		message := ""
		if v.Message != nil {
			message = *v.Message
		}
		fmt.Fprintf(out, "%-20s %-12s %-10d %-30s %s\n", v.AppID, v.State, v.RestartCount, hostnameList(v.Hostnames), message)
	}
}

func newAppsStatusCmd() *cobra.Command {
	var app string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show one app's reported state in full",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if rc := remoteClient(); rc != nil {
				view, err := rc.AppStatus(app)
				if err != nil {
					return err
				}
				printAppStatus(cmd.OutOrStdout(), *view)
				return nil
			}

			reported, err := readReported()
			if err != nil {
				return err
			}
			hostnames := readDesiredHostnames()
			for _, inst := range reported.Instances {
				if inst.AppID != app {
					continue
				}
				printAppStatus(cmd.OutOrStdout(), remoteapi.AppView{ReportedInstance: inst, Hostnames: hostnames[inst.AppID]})
				return nil
			}
			return fmt.Errorf("no app named %q on this host", app)
		},
	}
	cmd.Flags().StringVar(&app, "app", "", "the app to show (required)")
	cmd.MarkFlagRequired("app")
	return cmd
}

func printAppStatus(out io.Writer, v remoteapi.AppView) {
	field(out, "appId", v.AppID)
	field(out, "deploymentId", v.DeploymentID)
	field(out, "state", v.State)
	if v.HostPort != nil {
		field(out, "hostPort", *v.HostPort)
	} else {
		field(out, "hostPort", "(none, no slot held)")
	}
	if v.GuestIpv4 != nil {
		field(out, "guestIpv4", *v.GuestIpv4)
	} else {
		field(out, "guestIpv4", "(none, no slot held)")
	}
	field(out, "hostnames", hostnameList(v.Hostnames))
	field(out, "restartCount", v.RestartCount)
	if v.StartedAt != nil {
		field(out, "startedAt", *v.StartedAt)
	}
	if v.Message != nil {
		field(out, "message", *v.Message)
	}
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
			var data string
			if rc := remoteClient(); rc != nil {
				var err error
				data, err = rc.Logs(app, lines)
				if err != nil {
					return err
				}
			} else {
				path := fmt.Sprintf("/var/lib/nibrunner/logs/%s.log", app)
				raw, err := remote.ReadFile(path)
				if errors.Is(err, remote.ErrNotExist) {
					return fmt.Errorf("no log file yet for %q: nibrunnerd writes one once it boots the instance, which happens on its own reconcile pass after `nibr run` returns, not immediately; check `nibr apps status --app %s` and try again shortly", app, app)
				}
				if err != nil {
					return fmt.Errorf("reading %s: %w", path, err)
				}
				data = string(raw)
			}

			logLines := strings.Split(strings.TrimRight(data, "\n"), "\n")
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
			if rc := remoteClient(); rc != nil {
				if err := rc.DeleteApp(app, keepVolume); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "removed %s from %s's desired.json; nibrunnerd will tear it down on its next pass\n", app, remoteURL)
				return nil
			}

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
