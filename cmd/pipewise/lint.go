package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/damienaulombard/pipewise/internal/eval"
	"github.com/damienaulombard/pipewise/internal/fix"
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
	platform      string
	fix           bool
	fixExpireIn   string
}

func newLintCmd() *cobra.Command {
	var o lintOptions
	cmd := &cobra.Command{
		Use:   "lint <file|->",
		Short: "Analyse a GitLab CI configuration or a GitHub Actions workflow",
		Long: "Analyse a GitLab CI configuration and report jobs that run more often than they need to.\n" +
			"Pass - to read the configuration from standard input.\n" +
			"A GitHub Actions workflow is parsed and its problems reported; its jobs are not evaluated yet.\n\n" +
			"With --fix, the report is replaced by a unified diff of the file that fixes the findings of\n" +
			"missing-interruptible and artifacts-no-expiry; the file itself is not modified:\n" +
			"  pipewise lint --fix .gitlab-ci.yml | git apply\n" +
			"A finding the diff cannot fix safely is named on standard error, with the reason.\n\n" +
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
	f.StringVar(&o.platform, "platform", "auto", platformUsage)
	f.BoolVar(&o.fix, "fix", false,
		"print a unified diff that fixes missing-interruptible and artifacts-no-expiry, instead of the report")
	f.StringVar(&o.fixExpireIn, "fix-expire-in", fix.DefaultExpireIn,
		"the expire_in --fix adds to artifacts")
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
	opts, err := projectOptions(o.defaultBranch, o.skipBranch)
	if err != nil {
		return err
	}
	platform, err := detectPlatform(o.platform, path)
	if err != nil {
		return err
	}
	if err := checkFixOptions(cmd, path, platform, o); err != nil {
		return err
	}

	data, name, err := readConfig(cmd.InOrStdin(), path)
	if err != nil {
		return err
	}
	var outcome *rules.Outcome
	if platform == platformGitHub {
		outcome = rules.AnalyzeWorkflow(data, workflowReader(path))
	} else {
		outcome = rules.Analyze(data, opts)
	}
	if o.fix {
		if err := writeFix(cmd, path, data, outcome, o.fixExpireIn); err != nil {
			return err
		}
	} else if err := render(cmd.OutOrStdout(), name, outcome); err != nil {
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

// checkFixOptions checks --fix and --fix-expire-in.
func checkFixOptions(cmd *cobra.Command, path, platform string, o lintOptions) error {
	if !o.fix {
		if cmd.Flags().Changed("fix-expire-in") {
			return fmt.Errorf("--fix-expire-in only applies with --fix")
		}
		return nil
	}
	switch {
	case path == "-":
		return fmt.Errorf("--fix needs a file: the diff names the file it applies to")
	case platform == platformGitHub:
		return fmt.Errorf("--fix applies to GitLab CI configurations only")
	case strings.TrimSpace(o.fixExpireIn) == "":
		return fmt.Errorf("--fix-expire-in cannot be empty")
	case strings.ContainsAny(o.fixExpireIn, "\r\n"):
		return fmt.Errorf("--fix-expire-in must be on one line")
	}
	return nil
}

// writeFix prints the diff that fixes what it can of outcome, and names on
// standard error the findings it leaves.
func writeFix(cmd *cobra.Command, path string, data []byte, outcome *rules.Outcome, expireIn string) error {
	res := fix.Fix(filepath.ToSlash(filepath.Clean(path)), data, outcome.Findings, fix.Options{ExpireIn: expireIn})
	for _, s := range res.Skipped {
		where := "default:"
		if s.Job != "" {
			where = fmt.Sprintf("job %q", s.Job)
		}
		// Like the report, a message stderr cannot take has nowhere else to go.
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "pipewise: --fix skipped %s on %s (line %d): %s\n",
			s.Rule, where, s.Line, s.Reason)
	}
	if _, err := io.WriteString(cmd.OutOrStdout(), res.Diff); err != nil {
		return fmt.Errorf("write the diff: %w", err)
	}
	return nil
}

// projectOptions checks the flags that describe the project around the
// configuration.
func projectOptions(defaultBranch, skipBranch string) (eval.Options, error) {
	skip, ok := settingValues[skipBranch]
	if !ok {
		return eval.Options{}, fmt.Errorf("--skip-branch-pipelines-for-mrs must be true, false or unknown, not %q", skipBranch)
	}
	if defaultBranch == "" {
		return eval.Options{}, fmt.Errorf("--default-branch cannot be empty")
	}
	return eval.Options{DefaultBranch: defaultBranch, SkipBranchPipelinesForMRs: skip}, nil
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
