package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestCommandHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"workflows", "--help"}, {"workflows", "cancel-all", "--help"}} {
		var out, stderr bytes.Buffer
		if err := run(context.Background(), args, &out, &stderr); err != nil {
			t.Fatalf("help: %v", err)
		}
		if !strings.Contains(out.String(), "gh kit") || !strings.Contains(out.String(), "workflows") {
			t.Fatalf("unexpected help: %s", &out)
		}
		if stderr.Len() != 0 {
			t.Fatalf("unexpected stderr: %s", &stderr)
		}
	}
}

func TestRunInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{}, {"cancel-all-workflows", "--pr", "42"}, {"workflows", "cancel-all"},
		{"workflows", "cancel-all", "--pr", "0"}, {"workflows", "cancel-all", "--pr", "-1"},
		{"workflows", "cancel-all", "--pr", "abc"}, {"workflows", "cancel-all", "--pr", "42", "extra"},
		{"workflows"}, {"workflows", "unknown"}, {"workflows", "cancel-all", "--unknown"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			if err := run(context.Background(), args, &out, &stderr); err == nil {
				t.Fatal("expected invalid command error")
			}
			if out.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("errors must be returned, not printed: stdout=%q stderr=%q", &out, &stderr)
			}
		})
	}
}

func TestRunHelpDoesNotLeakArguments(t *testing.T) {
	if err := run(context.Background(), []string{"--help"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"workflows", "cancel-all"}, io.Discard, io.Discard); err == nil {
		t.Fatal("expected missing --pr error on fresh invocation")
	}
}
