package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newHostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Inspect this host itself, not one of its apps",
	}
	cmd.AddCommand(newHostStatusCmd())
	return cmd
}

func newHostStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show which desired.json this host has taken up, and its capacity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reported, err := readReported()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			field(out, "hostId", reported.HostID)
			field(out, "state", reported.State)
			field(out, "reportedAt", reported.ReportedAt)
			if reported.AcceptedDigest != nil {
				field(out, "acceptedDigest", *reported.AcceptedDigest)
			} else {
				field(out, "acceptedDigest", "(none, no document taken up yet)")
			}
			if reported.AcceptedRevision != nil {
				field(out, "acceptedRevision", *reported.AcceptedRevision)
			} else {
				field(out, "acceptedRevision", "(none, the document taken up named none)")
			}
			field(out, "capacity", fmt.Sprintf("%d vCPU, %d MiB", reported.Capacity.VCPUCount, reported.Capacity.MemoryMib))
			field(out, "allocatable", fmt.Sprintf("%d vCPU, %d MiB", reported.Allocatable.VCPUCount, reported.Allocatable.MemoryMib))
			if reported.Message != nil {
				field(out, "message", *reported.Message)
			}
			return nil
		},
	}
}
