// Package cmd wires nibr's command tree with cobra. By default every command still runs
// locally, on the same host as nibrunnerd, writing /var/lib/nibrunner/desired.json the only way
// nibrunnerd's own proxy code trusts: as a local file write by the operator already on this box,
// atomically, whole. --remote is the one opt-in exception: it points nibr at a nibrunner-api
// instance (a separate, optional HTTP wrapper, co-located with the target nibrunnerd) instead,
// so these same commands can run from off-box. It changes nibr's own transport, not what
// nibrunnerd itself will accept.
package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"nibrunner-cli/internal/remoteapi"
)

// remoteURL and remoteToken back the --remote/--remote-token persistent flags, read by every
// subcommand's RunE (flag values are only populated after cobra parses them, so these can't be
// read at command-construction time).
var (
	remoteURL   string
	remoteToken string
)

// remoteClient returns a nibrunner-api client if --remote (or NIBR_REMOTE_URL) names one, or nil
// if nibr should operate locally as it always has. Every command checks this once, at the top of
// its RunE, and branches: local behavior is unchanged either way.
func remoteClient() *remoteapi.Client {
	if remoteURL == "" {
		return nil
	}
	return remoteapi.New(remoteURL, remoteToken)
}

func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "nibr",
		Short: "Deploy a binary to the nibrunner host it runs on",
		Long: `nibr writes /var/lib/nibrunner/desired.json the only way nibrunnerd's own proxy
code trusts: as a local file write by the operator already on this box, atomically, whole.
That remains the default. --remote is the opt-in exception: it points these same commands at a
nibrunner-api instance over HTTP instead, for running them from off-box. nibrunnerd itself still
only ever sees a local, atomic, whole-document write performed by whatever nibrunner-api does on
its end.`,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&remoteURL, "remote", os.Getenv("NIBR_REMOTE_URL"),
		"nibrunner-api base URL (e.g. http://host:8282); defaults to $NIBR_REMOTE_URL. Unset: nibr operates on this host's local files, as always")
	root.PersistentFlags().StringVar(&remoteToken, "remote-token", os.Getenv("NIBR_REMOTE_TOKEN"),
		"bearer token for --remote; defaults to $NIBR_REMOTE_TOKEN")
	root.AddCommand(newRunCmd())
	root.AddCommand(newAppsCmd())
	root.AddCommand(newHostCmd())
	return root
}
