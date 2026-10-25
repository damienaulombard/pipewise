package main

import (
	"bytes"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "default build", version: "dev", want: "pipewise dev\n"},
		{name: "release build", version: "v0.1.0", want: "pipewise v0.1.0\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved := version
			t.Cleanup(func() { version = saved })
			version = tt.version

			var out bytes.Buffer
			cmd := newRootCmd(&out)
			cmd.SetArgs([]string{"version"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("version: unexpected error: %v", err)
			}
			if got := out.String(); got != tt.want {
				t.Errorf("version output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionCommandRejectsArguments(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version", "extra"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("version with an argument: expected an error, got none")
	}
}
