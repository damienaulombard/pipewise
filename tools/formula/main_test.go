package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden formula")

const sumLine = "  pipewise_v1.2.3_"

func hash(c string) string { return strings.Repeat(c, 64) }

func validSums() string {
	return hash("1") + sumLine + "darwin_amd64.tar.gz\n" +
		hash("2") + sumLine + "darwin_arm64.tar.gz\n" +
		hash("3") + sumLine + "linux_amd64.tar.gz\n" +
		hash("4") + sumLine + "linux_arm64.tar.gz\n"
}

func TestGenerateGolden(t *testing.T) {
	sums, err := os.ReadFile(filepath.Join("testdata", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := generate("damienaulombard/pipewise", "v1.2.3", sums)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	golden := filepath.Join("testdata", "pipewise.rb")
	if *update {
		if err := os.WriteFile(golden, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("formula differs from %s (go test ./tools/formula -update to rewrite):\n%s", golden, got)
	}
}

func TestGenerateRefuses(t *testing.T) {
	tests := []struct {
		name string
		repo string
		tag  string
		sums string
		want string
	}{
		{"tag without v", "damienaulombard/pipewise", "1.2.3", validSums(), `tag "1.2.3"`},
		{"tag with a suffix", "damienaulombard/pipewise", "v1.2.3-rc.1", validSums(), `tag "v1.2.3-rc.1"`},
		{"tag with a quote", "damienaulombard/pipewise", `v1.2.3"`, validSums(), "tag"},
		{"repository not owner/name", "pipewise", "v1.2.3", validSums(), `repository "pipewise"`},
		{"repository with a quote", `a/b"`, "v1.2.3", validSums(), "repository"},
		{"missing platform", "damienaulombard/pipewise", "v1.2.3",
			strings.Replace(validSums(), hash("4")+sumLine+"linux_arm64.tar.gz\n", "", 1),
			"pipewise_v1.2.3_linux_arm64.tar.gz"},
		{"checksums of another version", "damienaulombard/pipewise", "v1.2.4", validSums(),
			"pipewise_v1.2.4_darwin_arm64.tar.gz"},
		{"short hash", "damienaulombard/pipewise", "v1.2.3",
			strings.Replace(validSums(), hash("1"), "abc", 1), "line 1"},
		{"hash not hex", "damienaulombard/pipewise", "v1.2.3",
			strings.Replace(validSums(), hash("2"), hash("g"), 1), "line 2"},
		{"line without a file", "damienaulombard/pipewise", "v1.2.3", hash("1") + "\n" + validSums(), "line 1"},
		{"same archive twice", "damienaulombard/pipewise", "v1.2.3",
			validSums() + hash("5") + sumLine + "linux_arm64.tar.gz\n", "line 5"},
		{"empty", "damienaulombard/pipewise", "v1.2.3", "", "pipewise_v1.2.3_darwin_arm64.tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generate(tt.repo, tt.tag, []byte(tt.sums))
			if err == nil {
				t.Fatalf("no error, formula:\n%s", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestGenerateAccepts(t *testing.T) {
	tests := []struct {
		name string
		sums string
	}{
		{"binary mode marker", strings.ReplaceAll(validSums(), "  pipewise", " *pipewise")},
		{"CRLF and blank lines", "\r\n" + strings.ReplaceAll(validSums(), "\n", "\r\n") + "\n"},
		{"upper-case hash", strings.Replace(validSums(), hash("1"), hash("A"), 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := generate("damienaulombard/pipewise", "v1.2.3", []byte(tt.sums)); err != nil {
				t.Errorf("generate: %v", err)
			}
		})
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	sums := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sums, []byte(validSums()), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"writes the formula", []string{"-tag", "v1.2.3", "-sums", sums}, 0, `version "1.2.3"`, ""},
		{"other repository", []string{"-repo", "o/r", "-tag", "v1.2.3", "-sums", sums}, 0,
			"https://github.com/o/r/releases/download/v1.2.3/", ""},
		{"missing flags", nil, 2, "", "-tag and -sums are required"},
		{"unknown flag", []string{"-nope"}, 2, "", "-nope"},
		{"unreadable checksums", []string{"-tag", "v1.2.3", "-sums", filepath.Join(dir, "absent")}, 1, "", "absent"},
		{"invalid tag", []string{"-tag", "latest", "-sums", sums}, 1, "", `tag "latest"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("code %d, want %d (stderr: %s)", code, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantOut) {
				t.Errorf("stdout does not contain %q:\n%s", tt.wantOut, stdout.String())
			}
			if tt.wantErr != "" && !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("stderr does not contain %q:\n%s", tt.wantErr, stderr.String())
			}
			if tt.wantCode != 0 && stdout.Len() != 0 {
				t.Errorf("stdout not empty on failure:\n%s", stdout.String())
			}
		})
	}
}
