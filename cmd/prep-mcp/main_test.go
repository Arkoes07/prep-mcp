package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
