package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/packtrix/packtrix/internal/version"
)

func newVersionCommand() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of packtrix",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			info := version.Get()
			if short {
				fmt.Fprintln(cmd.OutOrStdout(), info.Version)
				return
			}
			fmt.Fprintln(cmd.OutOrStdout(), info)
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "print only the version number")
	return cmd
}
