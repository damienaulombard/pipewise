package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/damienaulombard/pipewise/internal/config"
	"github.com/damienaulombard/pipewise/internal/eval"
	"github.com/damienaulombard/pipewise/internal/report"
	"github.com/damienaulombard/pipewise/internal/rules"
)

type explainOptions struct {
	format        string
	defaultBranch string
	skipBranch    string
}

func newExplainCmd() *cobra.Command {
	var o explainOptions
	cmd := &cobra.Command{
		Use:   "explain <job> [file|-]",
		Short: "Show what decides whether a job runs on each event",
		Long: "Show, for a merge request, a push to the default branch and a tag, whether a job runs\n" +
			"and which part of the configuration decides it.\n" +
			"The file defaults to .gitlab-ci.yml; pass - to read the configuration from standard input.\n\n" +
			"Exit codes: 0 on success, 2 on a usage or read error, or when the job does not exist.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ".gitlab-ci.yml"
			if len(args) == 2 {
				path = args[1]
			}
			return explain(cmd, args[0], path, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.format, "format", "text", "output format: text or json")
	f.StringVar(&o.defaultBranch, "default-branch", "main", "the project's default branch")
	f.StringVar(&o.skipBranch, "skip-branch-pipelines-for-mrs", "unknown",
		"the project setting of that name: true, false or unknown")
	return cmd
}

func explain(cmd *cobra.Command, job, path string, o explainOptions) error {
	render, ok := map[string]func(io.Writer, string, eval.JobResult) error{
		"text": report.ExplainText,
		"json": report.ExplainJSON,
	}[o.format]
	if !ok {
		return fmt.Errorf("--format must be text or json, not %q", o.format)
	}
	opts, err := projectOptions(o.defaultBranch, o.skipBranch)
	if err != nil {
		return err
	}

	data, name, err := readConfig(cmd.InOrStdin(), path)
	if err != nil {
		return err
	}
	outcome := rules.Analyze(data, opts)
	for _, jr := range outcome.Jobs {
		if jr.Job.Name != job {
			continue
		}
		if err := render(cmd.OutOrStdout(), name, jr); err != nil {
			return fmt.Errorf("write the explanation: %w", err)
		}
		return nil
	}
	return notExplained(job, name, outcome.Findings)
}

// notExplained says why job has no verdicts: a problem that kept it from
// being analysed, one that kept the whole file from being read, or no such
// job.
func notExplained(job, file string, findings []rules.Finding) error {
	var fileProblem string
	for _, f := range findings {
		if f.Rule != config.KindParseError {
			continue
		}
		msg := strings.TrimSuffix(f.Message, ".")
		if f.Job == job {
			return fmt.Errorf("job %q is not analysed: %s", job, msg)
		}
		if f.Job == "" && fileProblem == "" {
			fileProblem = msg
		}
	}
	if fileProblem != "" {
		return fmt.Errorf("no job %q in %s: %s", job, file, fileProblem)
	}
	return fmt.Errorf("no job %q in %s", job, file)
}
