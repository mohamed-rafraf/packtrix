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
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/packtrix/packtrix/internal/catalog"
)

func newCatalogCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Show the built-in distributions",
		Long: `The catalog is Packtrix's built-in knowledge of distributions: their
repositories, components and signing keys, pinned by fingerprint. It is what
lets a manifest say "distro: ubuntu-24.04".`,
	}
	cmd.AddCommand(newCatalogListCommand(), newCatalogShowCommand())
	return cmd
}

func newCatalogListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the built-in distributions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := catalog.Builtin()
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tRELEASE\tFORMAT\tARCHITECTURES")
			for _, d := range c.Distros() {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.Name, release(d), d.Format, strings.Join(d.Architectures, ", "))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "\nCatalog version %s\n", c.Version)
			return err
		},
	}
}

func newCatalogShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "show <distro>",
		Short:   "Show the repositories and keys of a distribution",
		Example: `  packtrix catalog show ubuntu-24.04`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := catalog.Builtin()
			if err != nil {
				return err
			}
			d, err := c.Distro(args[0])
			if err != nil {
				return err
			}
			return showDistro(cmd.OutOrStdout(), d)
		},
	}
}

func release(d *catalog.Distro) string {
	if d.Release.Codename == "" {
		return d.Release.Version
	}
	return fmt.Sprintf("%s (%s)", d.Release.Version, d.Release.Codename)
}

func showDistro(out io.Writer, d *catalog.Distro) error {
	var defaults, optional []string
	for _, c := range d.Components {
		if c.Default {
			defaults = append(defaults, c.Name)
		} else {
			optional = append(optional, c.Name)
		}
	}
	components := strings.Join(defaults, ", ")
	if len(optional) > 0 {
		components += "; optional: " + strings.Join(optional, ", ")
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", d.Name)
	fmt.Fprintf(w, "Release:\t%s %s\n", d.Family, release(d))
	fmt.Fprintf(w, "Format:\t%s\n", d.Format)
	fmt.Fprintf(w, "Architectures:\t%s\n", strings.Join(d.Architectures, ", "))
	fmt.Fprintf(w, "Components:\t%s\n", components)
	fmt.Fprintf(w, "Baseline:\t%s, component %s\n", d.Baseline.Repository, d.Baseline.Component)
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(out)
	w = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REPOSITORY\tSUITE\tKEYS\tARCHITECTURE\tURL")
	for _, r := range d.Repositories {
		for i, arch := range d.Architectures {
			name, suite, keys := r.Name, r.Suite, strings.Join(r.Keys, ", ")
			if i > 0 {
				name, suite, keys = "", "", ""
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, suite, keys, arch, r.URLFor(arch))
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(out)
	w = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tFINGERPRINT")
	var keys []string
	for k := range d.Keys {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "%s\t%s\n", k, d.Keys[k])
	}
	return w.Flush()
}
