package main

import (
	"fmt"

	"github.com/damienaulombard/pipewise/internal/actions"
)

// Platforms a configuration is read for.
const (
	platformGitLab = "gitlab"
	platformGitHub = "github"
)

const platformUsage = "CI platform: auto (a file under .github/workflows/ is GitHub Actions, " +
	"anything else GitLab CI, standard input included), gitlab or github"

// detectPlatform resolves --platform for path: auto picks GitHub Actions for
// a file in a .github/workflows directory and GitLab CI otherwise.
func detectPlatform(flag, path string) (string, error) {
	switch flag {
	case platformGitLab, platformGitHub:
		return flag, nil
	case "auto":
		if _, ok := actions.RepositoryRoot(path); ok {
			return platformGitHub, nil
		}
		return platformGitLab, nil
	default:
		return "", fmt.Errorf("--platform must be auto, gitlab or github, not %q", flag)
	}
}

// workflowReader reads the local reusable workflows of the repository
// holding path, or returns nil when that repository is unknown.
func workflowReader(path string) actions.ReadFunc {
	if path == "-" {
		return nil
	}
	root, ok := actions.RepositoryRoot(path)
	if !ok {
		return nil
	}
	return actions.DirReader(root)
}
