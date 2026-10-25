package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/damienaulombard/pipewise/internal/gitlab"
)

// fetchTimeout bounds one call to the GitLab API.
const fetchTimeout = 30 * time.Second

// invalidConfigError means GitLab itself rejects the configuration.
type invalidConfigError struct{ errors []string }

func (e *invalidConfigError) Error() string {
	return "GitLab reports the configuration as invalid:\n  - " + strings.Join(e.errors, "\n  - ")
}

func newFetchCmd() *cobra.Command {
	var project, ref, output string
	cmd := &cobra.Command{
		Use:   "fetch --project <group/project>",
		Short: "Download a project's merged CI configuration from GitLab",
		Long: "Download a project's CI configuration from the GitLab API, with every include expanded,\n" +
			"so that pipewise lint sees every job:\n\n" +
			"  pipewise fetch --project group/app | pipewise lint -\n\n" +
			"The token is read from GITLAB_TOKEN, and the instance from GITLAB_URL (default https://gitlab.com).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fetch(cmd, project, ref, output)
		},
	}
	f := cmd.Flags()
	f.StringVar(&project, "project", "", "project path, such as group/app, or numeric id (required)")
	f.StringVar(&ref, "ref", "", "branch, tag or commit to read the configuration from (default: head of the default branch)")
	f.StringVarP(&output, "output", "o", "", "write the configuration to this file instead of standard output")
	return cmd
}

// gitlabURL is the instance to talk to: GITLAB_URL, or GitLab.com.
func gitlabURL() string {
	if u := os.Getenv("GITLAB_URL"); u != "" {
		return u
	}
	return "https://gitlab.com"
}

func fetch(cmd *cobra.Command, project, ref, output string) error {
	if project == "" {
		return fmt.Errorf("--project is required")
	}
	base := gitlabURL()
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("GITLAB_URL must be an http or https URL, not %q", base)
	}

	client := &gitlab.Client{
		BaseURL:   base,
		Token:     os.Getenv("GITLAB_TOKEN"),
		UserAgent: "pipewise/" + version,
		HTTP:      &http.Client{Timeout: fetchTimeout},
	}
	lint, err := client.MergedConfig(cmd.Context(), project, ref)
	if err != nil {
		return err
	}
	for _, w := range lint.Warnings {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "pipewise: GitLab warning: %s\n", w)
	}
	if !lint.Valid {
		return &invalidConfigError{errors: lint.Errors}
	}

	yaml := lint.MergedYAML
	if !strings.HasSuffix(yaml, "\n") {
		yaml += "\n"
	}
	if output != "" {
		if err := os.WriteFile(output, []byte(yaml), 0o644); err != nil {
			return err
		}
		return nil
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), yaml); err != nil {
		return fmt.Errorf("write the configuration: %w", err)
	}
	return nil
}
