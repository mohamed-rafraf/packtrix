// Command packtrix builds offline APT and RPM repositories from a manifest.
package main

import (
	"fmt"
	"os"

	"github.com/packtrix/packtrix/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
