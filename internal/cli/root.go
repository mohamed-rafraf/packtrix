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
		newCatalogCommand(),
		newRenderCommand(),
		newValidateCommand(),
		newVersionCommand(),
	)
	return cmd
}

// addManifestFlag registers the -f flag on commands that read a manifest.
func addManifestFlag(cmd *cobra.Command, file *string) {
	cmd.Flags().StringVarP(file, "file", "f", DefaultManifest, "path to the manifest")
}
