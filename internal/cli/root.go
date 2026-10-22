// Package cli implements the packtrix command line.
package cli

import (
	"github.com/spf13/cobra"
)

// DefaultManifest is the manifest read when -f isn't given.
const DefaultManifest = "packtrix.yaml"

// NewRootCommand returns the packtrix command tree.
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "packtrix",
		Short: "Declarative offline APT and RPM repositories",
		Long: `Packtrix builds dependency-complete, signed APT and RPM repositories
from a single manifest, ready to carry into air-gapped environments.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(
		newValidateCommand(),
		newVersionCommand(),
	)
	return cmd
}

// addManifestFlag registers the -f flag on commands that read a manifest.
func addManifestFlag(cmd *cobra.Command, file *string) {
	cmd.Flags().StringVarP(file, "file", "f", DefaultManifest, "path to the manifest")
}
