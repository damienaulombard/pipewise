package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/damienaulombard/pipewise/internal/eval"
	"github.com/damienaulombard/pipewise/internal/report"
	"github.com/damienaulombard/pipewise/internal/rules"
)

var failOnLevels = map[string]rules.Severity{
	"error":   rules.Error,
	"warning": rules.Warning,
	"info":    rules.Info,
}

var settingValues = map[string]eval.Truth{
	"true":    eval.True,
	"false":   eval.False,
	"unknown": eval.Unknown,
}

type lintOptions struct {
	format        string
	failOn        string
	defaultBranch string
	skipBranch    string
}

func newLintCmd() *cobra.Command {
	var o lintOptions
	cmd := &cobra.Command{
		Use:   "lint <file|->",
		Short: "Analyse a GitLab CI configuration",
		Long: "Analyse a GitLab CI configuration and report jobs that run more often than they need to.\n" +
			"Pass - to read the configuration from standard input.\n\n" +
			"Exit codes: 0 when no finding reaches --fail-on, 1 when one does, 2 on a usage or read error.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return lint(cmd, args[0], o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.format, "format", "text", "output format: text or json")
	f.StringVar(&o.failOn, "fail-on", "error",
		"lowest severity that makes the command fail: error, warning, info or none")
	f.StringVar(&o.defaultBranch, "default-branch", "main", "the project's default branch")
	f.StringVar(&o.skipBranch, "skip-branch-pipelines-for-mrs", "unknown",
		"the project setting of that name: true, false or unknown")
	return cmd
}

func lint(cmd *cobra.Command, path string, o lintOptions) error {
	render, ok := map[string]func(io.Writer, string, *rules.Outcome) error{
		"text": report.Text,
		"json": report.JSON,
	}[o.format]
	if !ok {
		return fmt.Errorf("--format must be text or json, not %q", o.format)
	}
	threshold, ok := failOnLevels[o.failOn]
	if !ok && o.failOn != "none" {
		return fmt.Errorf("--fail-on must be error, warning, info or none, not %q", o.failOn)
	}
	skip, ok := settingValues[o.skipBranch]
	if !ok {
		return fmt.Errorf("--skip-branch-pipelines-for-mrs must be true, false or unknown, not %q", o.skipBranch)
	}
	if o.defaultBranch == "" {
		return fmt.Errorf("--default-branch cannot be empty")
	}

	data, name, err := readConfig(cmd.InOrStdin(), path)
	if err != nil {
		return err
	}
	outcome := rules.Analyze(data, eval.Options{
		DefaultBranch:             o.defaultBranch,
		SkipBranchPipelinesForMRs: skip,
	})
	if err := render(cmd.OutOrStdout(), name, outcome); err != nil {
		return fmt.Errorf("write the report: %w", err)
	}

	if o.failOn == "none" {
		return nil
	}
	n := 0
	for _, finding := range outcome.Findings {
		if finding.Severity >= threshold {
			n++
		}
	}
	if n > 0 {
		return &findingsError{count: n}
	}
	return nil
}

// readConfig reads path, or standard input for "-". It returns the name to
// show in the report.
func readConfig(stdin io.Reader, path string) ([]byte, string, error) {
	if path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, "", fmt.Errorf("read standard input: %w", err)
		}
		return data, "<stdin>", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	return data, path, nil
}
