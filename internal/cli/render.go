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
	"path/filepath"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/expand"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

func newRenderCommand() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Print the targets as Packtrix will use them",
		Long: `Render expands the manifest with the built-in catalog and prints, for each
target and architecture, the repositories with their URLs, suites, components
and key fingerprints, and the packages that apply to the target.

It doesn't access the network: the repositories aren't read yet.`,
		Example: `  packtrix render
  packtrix render -f examples/k8s-node-prereqs.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := manifest.LoadFile(file)
			if err != nil {
				return err
			}
			c, err := catalog.Builtin()
			if err != nil {
				return err
			}
			res, err := expand.Expand(m, c, filepath.Dir(file))
			if err != nil {
				return err
			}
			out, err := yaml.Marshal(res)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
	addManifestFlag(cmd, &file)
	return cmd
}
