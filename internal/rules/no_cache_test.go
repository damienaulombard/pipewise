package rules

import "testing"

func TestNoCache(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		// managers is the end of the message, empty when nothing is reported.
		managers string
	}{
		{name: "npm ci", yaml: "build:\n  script: [npm ci, npm run build]\n", managers: "npm"},
		{name: "npm install with flags", yaml: "build:\n  script: [npm install --no-audit]\n", managers: "npm"},
		{name: "pnpm install", yaml: "build:\n  script: [pnpm install --frozen-lockfile]\n", managers: "pnpm"},
		{name: "yarn alone", yaml: "build:\n  script: [yarn]\n", managers: "yarn"},
		{name: "pip install", yaml: "build:\n  script: [pip3 install -r requirements.txt]\n", managers: "pip"},
		{name: "python -m pip install", yaml: "build:\n  script: [python -m pip install .]\n", managers: "pip"},
		{name: "composer install", yaml: "build:\n  script: [composer install]\n", managers: "composer"},
		{name: "go mod download", yaml: "build:\n  script: [go mod download]\n", managers: "go"},
		{name: "bundle install", yaml: "build:\n  script: [bundle install]\n", managers: "bundler"},
		{name: "maven wrapper", yaml: "build:\n  script: [./mvnw -B verify]\n", managers: "maven"},
		{name: "gradle wrapper", yaml: "build:\n  script: [./gradlew build]\n", managers: "gradle"},
		{
			name:     "after cd, an env var and sudo",
			yaml:     "build:\n  script:\n    - cd web && CI=1 sudo npm ci\n",
			managers: "npm",
		},
		{
			name:     "several package managers, in order, once each",
			yaml:     "build:\n  script:\n    - |\n      pip install -r requirements.txt\n      npm ci; npm ci\n",
			managers: "pip and npm",
		},
		{
			name:     "install in an inherited before_script",
			yaml:     "default:\n  before_script: [yarn install]\nbuild:\n  script: [yarn build]\n",
			managers: "yarn",
		},
		{name: "own cache", yaml: "build:\n  cache: {paths: [.npm/]}\n  script: [npm ci]\n"},
		{name: "cache from default:", yaml: "default:\n  cache: {paths: [.npm/]}\nbuild:\n  script: [npm ci]\n"},
		{name: "cache: [] is a decision", yaml: "build:\n  cache: []\n  script: [npm ci]\n"},
		{name: "no install step", yaml: "build:\n  script: [npm run lint, go test ./..., echo npm ci, pip --version]\n"},
		{
			name: "trigger job, even with an inherited install",
			yaml: "default:\n  before_script: [npm ci]\ndownstream:\n  trigger: {project: group/other}\n",
		},
		{name: "version and help flags", yaml: "build:\n  script: [yarn --version, mvn -v, ./gradlew --help, npm -h]\n"},
		{name: "a package manager without a subcommand", yaml: "build:\n  script: [npm, pip --no-cache-dir]\n"},
		{name: "yarn with only flags", yaml: "build:\n  script: [yarn --frozen-lockfile]\n", managers: "yarn"},
		{name: "add", yaml: "build:\n  script: [yarn add left-pad]\n", managers: "yarn"},
		{
			name:     "|| and | separators, python3 and a path",
			yaml:     "build:\n  script:\n    - test -d vendor || composer install\n    - echo ok | ./venv/bin/python3 -m pip install .\n",
			managers: "composer and pip",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var want []brief
			if tt.managers != "" {
				line := 1
				if tt.name == "install in an inherited before_script" {
					line = 3
				}
				want = []brief{{Info, "build", line,
					`Job "build" installs dependencies with ` + tt.managers + " but has no cache."}}
			}
			assertFindings(t, tt.yaml, "no-cache", want)
		})
	}
}

func TestNoCacheSuggestion(t *testing.T) {
	got := assertFindings(t, "build:\n  script: [npm ci]\n", "no-cache", []brief{
		{Info, "build", 1, `Job "build" installs dependencies with npm but has no cache.`},
	})
	want := `build:
  variables:
    npm_config_cache: $CI_PROJECT_DIR/.npm
  cache:
    - key:
        files: [package-lock.json]
      paths: [.npm/]
`
	if got[0].Suggestion != want {
		t.Errorf("suggestion:\n%s\nwant:\n%s", got[0].Suggestion, want)
	}

	// One cache per package manager; pnpm needs a command, not a variable.
	got = assertFindings(t, "build:\n  script: [pnpm install, go mod download]\n", "no-cache", []brief{
		{Info, "build", 1, `Job "build" installs dependencies with pnpm and go but has no cache.`},
	})
	want = `build:
  variables:
    GOMODCACHE: $CI_PROJECT_DIR/.go/pkg/mod
  # pnpm: run "pnpm config set store-dir .pnpm-store" before installing.
  cache:
    - key:
        files: [pnpm-lock.yaml]
      paths: [.pnpm-store/]
    - key:
        files: [go.sum]
      paths: [.go/pkg/mod/]
`
	if got[0].Suggestion != want {
		t.Errorf("suggestion:\n%s\nwant:\n%s", got[0].Suggestion, want)
	}
}
