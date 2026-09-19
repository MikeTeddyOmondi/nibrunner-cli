// Package cmd wires nibr's command tree with cobra. Every command runs locally, on the same host
// as nibrunnerd: there is no --host flag, because there is no remote to name.
package cmd

import "github.com/spf13/cobra"

func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "nibr",
		Short: "Deploy a binary to the nibrunner host it runs on",
		Long: `nibr writes /var/lib/nibrunner/desired.json the only way nibrunnerd's own proxy
code trusts: as a local file write by the operator already on this box, atomically, whole.
It runs on the same host as nibrunnerd; there is nothing here for a remote target.`,
		SilenceUsage: true,
	}
	root.AddCommand(newRunCmd())
	root.AddCommand(newAppsCmd())
	return root
}
