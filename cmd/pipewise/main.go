// Command pipewise analyses GitLab CI configurations and reports jobs that
// run more often than they need to.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time with -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	if err := newRootCmd(os.Stdout).Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd(out io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:          "pipewise",
		Short:        "Find CI jobs that run more often than they need to",
		SilenceUsage: true,
	}
	root.SetOut(out)
	root.AddCommand(newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the pipewise version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "pipewise %s\n", version)
			return err
		},
	}
}
