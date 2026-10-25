// Package gitlab is a minimal GitLab API client: it fetches the merged CI
// configuration of a project.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client talks to one GitLab instance.
type Client struct {
	// BaseURL is the instance, such as https://gitlab.com.
	BaseURL string
	// Token is a personal, project or group access token. It is sent in the
	// PRIVATE-TOKEN header and never written anywhere else.
	Token     string
	UserAgent string
	HTTP      *http.Client
}

// Lint is GitLab's answer about a project's configuration.
type Lint struct {
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors"`
	Warnings   []string `json:"warnings"`
	MergedYAML string   `json:"merged_yaml"`
}

// StatusError is a response GitLab answered with an error status.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	var hint string
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		hint = "check GITLAB_TOKEN: it is missing, expired, or cannot read this project's CI configuration"
	case http.StatusNotFound:
		hint = "the project or the ref does not exist, or the token cannot see it"
	default:
		hint = "unexpected answer"
	}
	msg := fmt.Sprintf("GitLab answered %d: %s", e.Status, hint)
	if e.Message != "" {
		msg += " (" + e.Message + ")"
	}
	return msg
}

// MergedConfig returns the configuration of project, a path such as
// "group/app" or a numeric id, with every include expanded. ref is the
// branch, tag or commit to read it from; empty means the head of the
// default branch.
func (c *Client) MergedConfig(ctx context.Context, project, ref string) (*Lint, error) {
	u := strings.TrimRight(c.BaseURL, "/") + "/api/v4/projects/" + url.PathEscape(project) + "/ci/lint"
	if ref != "" {
		u += "?" + url.Values{"content_ref": {ref}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build the request: %w", err)
	}
	if c.Token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.Token)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach GitLab: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read GitLab's answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message any `json:"message"`
		}
		_ = json.Unmarshal(body, &e) // the message is a hint; a body that is not JSON has none
		se := &StatusError{Status: resp.StatusCode}
		if e.Message != nil {
			se.Message = fmt.Sprint(e.Message)
		}
		return nil, se
	}
	var lint Lint
	if err := json.Unmarshal(body, &lint); err != nil {
		return nil, fmt.Errorf("read GitLab's answer: %w", err)
	}
	return &lint, nil
}
