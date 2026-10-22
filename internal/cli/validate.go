/*
Copyright 2026 The Packtrix Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
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
			m, err := manifest.LoadFile(file)
			if err != nil {
				return err
			}
			if output == "yaml" {
				out, err := manifest.Marshal(m)
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
