package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/write"
)

func TestRunHelpIsNotAnError(t *testing.T) {
	var stderr bytes.Buffer
	getenv := func(string) string { return "" }
	if err := run(context.Background(), []string{"-h"}, getenv, &stderr); err != nil {
		t.Fatalf("run -h: %v, want nil", err)
	}
	if !strings.Contains(stderr.String(), "-root") {
		t.Errorf("run -h printed %q, want the usage with -root", stderr.String())
	}
}

func TestRunUnknownFlagFails(t *testing.T) {
	var stderr bytes.Buffer
	getenv := func(string) string { return "" }
	if err := run(context.Background(), []string{"-nope"}, getenv, &stderr); err == nil {
		t.Fatal("run -nope: nil error, want one for the unknown flag")
	}
}

// newGit decides once whether writes are committed: no Git at all when
// commits are off, else one for the vault folder and the configured author.
func TestNewGit(t *testing.T) {
	tests := []struct {
		name, yaml string
		want       *write.Git
	}{
		{name: "commits_off", yaml: "commit_on_write: false\nauthor: Ada <ada@local>\n", want: nil},
		{
			name: "commits_on",
			yaml: "commit_on_write: true\nauthor: Ada Lovelace <ada@local>\n",
			want: &write.Git{Dir: "/vault", Name: "Ada Lovelace", Email: "ada@local"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatal(err)
			}
			got := newGit(cfg, "/vault")
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("newGit = %+v, want %+v", got, tc.want)
			}
		})
	}
}
