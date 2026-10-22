package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/packtrix/packtrix/pkg/apis/packtrix/v1alpha1"
)

func newValidateCommand() *cobra.Command {
	var (
		file   string
		output string
	)
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check a manifest for errors",
		Long: `Validate decodes the manifest, applies defaults and checks every field.
It reports all problems at once and doesn't access the network.

With -o yaml it prints the manifest with every default filled in.`,
		Example: `  packtrix validate
  packtrix validate -f examples/mixed-fleet.yaml -o yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if output != "" && output != "yaml" {
				return fmt.Errorf("unsupported output format %q (supported: yaml)", output)
			}
			m, err := v1alpha1.LoadFile(file)
			if err != nil {
				return err
			}
			if output == "yaml" {
				out, err := yaml.Marshal(m)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(out)
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is valid: %d target(s), %d package(s)\n",
				file, len(m.Spec.Targets), len(m.Spec.Packages))
			return nil
		},
	}
	addManifestFlag(cmd, &file)
	cmd.Flags().StringVarP(&output, "output", "o", "", "print the manifest with defaults applied (yaml)")
	return cmd
}
