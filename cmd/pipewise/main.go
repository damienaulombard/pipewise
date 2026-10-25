// Command pipewise analyses GitLab CI configurations and reports jobs that
// run more often than they need to.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time with -ldflags "-X main.version=<tag>".
var version = "dev"

// Exit codes.
const (
	exitOK = 0
	// exitFindings: the check failed, findings at or above --fail-on for
	// lint, or a configuration GitLab rejects for fetch.
	exitFindings = 1
	exitError    = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// findingsError means the analysis ran and found something at or above the
// --fail-on threshold. The report says what; nothing more is printed.
type findingsError struct{ count int }

func (e *findingsError) Error() string {
	return fmt.Sprintf("%d findings at or above the --fail-on threshold", e.count)
}

// run executes pipewise and returns its exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRootCmd(stdin, stdout, stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	var fe *findingsError
	var ic *invalidConfigError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &fe):
		return exitFindings
	case errors.As(err, &ic):
		_, _ = fmt.Fprintf(stderr, "pipewise: %v\n", err)
		return exitFindings
	default:
		// If stderr cannot be written to, there is nowhere left to say so;
		// the exit code still reports the failure.
		_, _ = fmt.Fprintf(stderr, "pipewise: %v\n", err)
		return exitError
	}
}

func newRootCmd(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "pipewise",
		Short:         "Find CI jobs that run more often than they need to",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newVersionCmd(), newLintCmd(), newFetchCmd())
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
