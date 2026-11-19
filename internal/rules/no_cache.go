package rules

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// noCache reports a job that installs dependencies with a known package
// manager and has no cache, own or inherited. An explicit empty cache is a
// decision and is not reported.
type noCache struct{}

func (noCache) ID() string { return "no-cache" }

// packageManager is how a package manager installs dependencies, and the
// cache that keeps them between runs.
type packageManager struct {
	name string
	// installs tells whether a command of the package manager installs
	// dependencies, from its arguments without flags.
	installs  func(args []string) bool
	lockfiles string
	paths     string
	// variable moves the package manager's cache into the project directory,
	// the only place GitLab caches; empty when it takes a command instead.
	variable string
	// note is a comment for what a variable cannot do.
	note string
}

func subcommand(names ...string) func([]string) bool {
	return func(args []string) bool {
		if len(args) == 0 {
			return false
		}
		for _, n := range names {
			if args[0] == n {
				return true
			}
		}
		return false
	}
}

func always([]string) bool { return true }

// packageManagers, by program name.
var packageManagers = func() map[string]*packageManager {
	npm := &packageManager{name: "npm", installs: subcommand("install", "i", "ci", "add"),
		lockfiles: "package-lock.json", paths: ".npm/", variable: "npm_config_cache: $CI_PROJECT_DIR/.npm"}
	pnpm := &packageManager{name: "pnpm", installs: subcommand("install", "i", "add"),
		lockfiles: "pnpm-lock.yaml", paths: ".pnpm-store/",
		note: `pnpm: run "pnpm config set store-dir .pnpm-store" before installing.`}
	yarn := &packageManager{name: "yarn",
		installs: func(args []string) bool {
			return len(args) == 0 || subcommand("install", "add")(args)
		},
		lockfiles: "yarn.lock", paths: ".yarn-cache/", variable: "YARN_CACHE_FOLDER: $CI_PROJECT_DIR/.yarn-cache"}
	pip := &packageManager{name: "pip", installs: subcommand("install"),
		lockfiles: "requirements.txt", paths: ".cache/pip/", variable: "PIP_CACHE_DIR: $CI_PROJECT_DIR/.cache/pip"}
	composer := &packageManager{name: "composer", installs: subcommand("install"),
		lockfiles: "composer.lock", paths: ".composer-cache/",
		variable: "COMPOSER_CACHE_DIR: $CI_PROJECT_DIR/.composer-cache"}
	golang := &packageManager{name: "go",
		installs: func(args []string) bool {
			return len(args) >= 2 && args[0] == "mod" && args[1] == "download"
		},
		lockfiles: "go.sum", paths: ".go/pkg/mod/", variable: "GOMODCACHE: $CI_PROJECT_DIR/.go/pkg/mod"}
	bundler := &packageManager{name: "bundler", installs: subcommand("install"),
		lockfiles: "Gemfile.lock", paths: "vendor/bundle/", variable: "BUNDLE_PATH: vendor/bundle"}
	maven := &packageManager{name: "maven", installs: always,
		lockfiles: "pom.xml", paths: ".m2/repository/",
		variable: "MAVEN_OPTS: -Dmaven.repo.local=$CI_PROJECT_DIR/.m2/repository"}
	gradle := &packageManager{name: "gradle", installs: always,
		lockfiles: "build.gradle, build.gradle.kts", paths: ".gradle/caches/, .gradle/wrapper/",
		variable: "GRADLE_USER_HOME: $CI_PROJECT_DIR/.gradle"}
	return map[string]*packageManager{
		"npm": npm, "pnpm": pnpm, "yarn": yarn, "pip": pip, "pip3": pip, "composer": composer,
		"go": golang, "bundle": bundler, "mvn": maven, "mvnw": maven, "gradle": gradle, "gradlew": gradle,
	}
}()

// infoFlags print a version or help and install nothing, even for the
// package managers that resolve dependencies on any call.
var infoFlags = map[string]bool{"--version": true, "-v": true, "-version": true, "--help": true, "-h": true}

// separators split a shell line into simple commands.
var separators = regexp.MustCompile(`&&|\|\||[;|]`)

// envAssignment is a leading VAR=value of a command.
var envAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// installers returns the package managers a script installs dependencies
// with, in order of first use.
func installers(script []string) []*packageManager {
	var out []*packageManager
	seen := map[string]bool{}
	for _, entry := range script {
		for _, line := range strings.Split(entry, "\n") {
			for _, cmd := range separators.Split(line, -1) {
				pm := installer(strings.Fields(cmd))
				if pm != nil && !seen[pm.name] {
					seen[pm.name] = true
					out = append(out, pm)
				}
			}
		}
	}
	return out
}

func installer(words []string) *packageManager {
	for len(words) > 0 && (envAssignment.MatchString(words[0]) || words[0] == "sudo") {
		words = words[1:]
	}
	if len(words) == 0 {
		return nil
	}
	program := path.Base(words[0])
	var args []string
	for _, w := range words[1:] {
		if infoFlags[w] {
			return nil
		}
		if !strings.HasPrefix(w, "-") {
			args = append(args, w)
		}
	}
	// python -m pip install …: the module is the program.
	if (program == "python" || program == "python3") && len(args) > 0 && args[0] == "pip" {
		program, args = "pip", args[1:]
	}
	pm, ok := packageManagers[program]
	if !ok || !pm.installs(args) {
		return nil
	}
	return pm
}

func (r noCache) Check(a *Analysis) []Finding {
	var out []Finding
	for _, j := range a.Pipeline.Jobs {
		if j.Trigger || j.Cache {
			continue
		}
		pms := installers(j.Script)
		if len(pms) == 0 {
			continue
		}
		names := make([]string, len(pms))
		for i, pm := range pms {
			names[i] = pm.name
		}
		with := names[0]
		if len(names) > 1 {
			with = joinAnd(names)
		}
		out = append(out, Finding{
			Rule:     r.ID(),
			Severity: Info,
			Job:      j.Name,
			Line:     j.Line,
			Message:  fmt.Sprintf("Job %q installs dependencies with %s but has no cache.", j.Name, with),
			Explanation: "Without a cache, every run downloads the same packages again: time on each pipeline, " +
				"and a failure whenever the registry is slow or down. " +
				"A cache keyed on the lockfile is reused until the dependencies change.",
			Suggestion: cacheSuggestion(j.Name, pms),
		})
	}
	return out
}

func cacheSuggestion(job string, pms []*packageManager) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", job)
	var variables, notes []string
	for _, pm := range pms {
		if pm.variable != "" {
			variables = append(variables, pm.variable)
		}
		if pm.note != "" {
			notes = append(notes, pm.note)
		}
	}
	if len(variables) > 0 {
		b.WriteString("  variables:\n")
		for _, v := range variables {
			fmt.Fprintf(&b, "    %s\n", v)
		}
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "  # %s\n", n)
	}
	b.WriteString("  cache:\n")
	for _, pm := range pms {
		fmt.Fprintf(&b, "    - key:\n        files: [%s]\n      paths: [%s]\n", pm.lockfiles, pm.paths)
	}
	return b.String()
}
