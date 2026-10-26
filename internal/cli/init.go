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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/validation"
)

// starter is the manifest that init writes.
var starter = template.Must(template.New("starter").Parse(`# A Packtrix manifest: the machines you run, and the packages they need.
# Every field: https://packtrix.github.io/docs/manifest/
apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: {{ .Name }}
spec:
  # The kinds of machines, one repository each. packtrix catalog list shows
  # the distros. For arm64 machines, add architectures: [amd64, arm64].
  targets:
{{- range .Targets }}
    - distro: {{ . }}
{{- end }}

  # What the machines need. Packtrix adds the dependencies.
  packages:
{{- range .Packages }}
    - {{ . }}
{{- else }}
    - curl                # an example: list what your machines need
{{- end }}

  # The machines already have a minimal installation of their distro. If they
  # are containers, which have less, Packtrix can include everything:
  # resolve:
  #   baseline: none

  publish:
    # The OpenPGP private key that signs the repositories, as file:/path or
    # env:NAME. Keep it secret.
    signing:
      key: file:signing-key.asc
`))

func newInitCommand() *cobra.Command {
	var (
		file, name        string
		targets, packages []string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter manifest",
		Long: `Init writes a starter manifest for the distros of your machines, with
comments on what to change. It prints the manifest, or writes it with -f,
which never overwrites a file.

The manifest is named after its directory unless --name is set.`,
		Example: `  packtrix init --target ubuntu-24.04 --target debian-12 > packtrix.yaml
  packtrix init --target ubuntu-24.04 --package curl --package nginx -f packtrix.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := catalog.Builtin()
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				return fmt.Errorf("name the distros of your machines with --target, from: %s", strings.Join(distroNames(c), ", "))
			}
			for _, t := range targets {
				if _, err := c.Distro(t); err != nil {
					return fmt.Errorf("--target: %w", err)
				}
			}
			if name == "" {
				dir := filepath.Dir(file)
				if file == "" {
					dir = "."
				}
				name = defaultName(dir)
			}

			var out bytes.Buffer
			if err := starter.Execute(&out, struct {
				Name              string
				Targets, Packages []string
			}{name, targets, packages}); err != nil {
				return err
			}
			// Flags end up in the manifest: check what they made of it.
			m, err := manifest.Parse(out.Bytes())
			if err != nil {
				return err
			}
			if err := validation.ValidateOfflineRepository(m); err != nil {
				return err
			}
			if file == "" {
				_, err := cmd.OutOrStdout().Write(out.Bytes())
				return err
			}
			if err := writeNew(file, out.Bytes()); err != nil {
				return err
			}
			next := "run packtrix plan"
			if len(packages) == 0 {
				next = "list the packages your machines need, then " + next
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s. Next: %s.\n", file, next)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&targets, "target", nil, "a built-in distro of your machines, such as ubuntu-24.04; repeat it for each")
	cmd.Flags().StringSliceVar(&packages, "package", nil, "a package your machines need; repeat it for each")
	cmd.Flags().StringVar(&name, "name", "", "the manifest's name (default: its directory's name)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "write the manifest to this file instead of printing it")
	return cmd
}

func distroNames(c *catalog.Catalog) []string {
	var names []string
	for _, d := range c.Distros() {
		names = append(names, d.Name)
	}
	return names
}

var notName = regexp.MustCompile(`[^a-z0-9.-]+`)

// defaultName returns the name of dir as a manifest name, or "packtrix"
// when there is nothing left of it.
func defaultName(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	name := strings.Trim(notName.ReplaceAllString(strings.ToLower(filepath.Base(dir)), "-"), "-.")
	if name == "" || len(name) > 63 {
		return "packtrix"
	}
	return name
}

// writeNew writes data to a file that doesn't exist yet.
func writeNew(file string, data []byte) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists; init doesn't overwrite it", file)
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
