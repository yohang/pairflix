package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandHelp(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --help: %v", err)
	}

	if !strings.Contains(out.String(), "pairflix") {
		t.Errorf("help output does not mention pairflix:\n%s", out.String())
	}
}

func TestRootCommandVersion(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}

	if !strings.Contains(out.String(), version) {
		t.Errorf("version output %q does not contain %q", out.String(), version)
	}
}
