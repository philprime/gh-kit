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
		{"pr"}, {"pr", "unknown"}, {"pr", "comments"},
		{"pr", "comments", "--pr", "0"}, {"pr", "comments", "--pr", "-1"},
		{"pr", "comments", "--pr", "abc"}, {"pr", "comments", "--pr", "42", "extra"},
		{"pr", "comments", "--unknown"},
		{"workflows", "logs"}, {"workflows", "logs", "--job", "0"},
		{"workflows", "logs", "--job", "-1"}, {"workflows", "logs", "--job", "abc"},
		{"workflows", "logs", "--job", "124568269207", "extra"},
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

func TestWorkflowLogsHelp(t *testing.T) {
	var out, stderr bytes.Buffer
	if err := run(context.Background(), []string{"workflows", "logs", "--help"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "--job") {
		t.Fatalf("missing job flag in help: %s", &out)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", &stderr)
	}
}

func TestAgentSkillsCommands(t *testing.T) {
	for _, args := range [][]string{
		{"agent", "skills", "list"},
		{"agent", "skills", "get", "gh-kit"},
	} {
		var out, stderr bytes.Buffer
		if err := run(context.Background(), args, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "gh-kit") || stderr.Len() != 0 {
			t.Fatalf("unexpected output: stdout=%q stderr=%q", &out, &stderr)
		}
	}
}

func TestAgentSkillsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{}, {"skills"}, {"skills", "list", "extra"}, {"skills", "get"},
		{"skills", "get", "unknown"}, {"skills", "get", "gh-kit", "extra"},
		{"skills", "install"}, {"skills", "install", "--all", "gh-kit"},
		{"skills", "install", "gh-kit", "unknown"}, {"skills", "install", "../escape"},
		{"skills", "install", "gh-kit", "--project", "--dir", ""},
		{"skills", "install", "gh-kit", "--dir", " "},
		{"skills", "uninstall"}, {"skills", "uninstall", "--all"},
		{"skills", "uninstall", "gh-kit", "--force"},
		{"skills", "uninstall", "gh-kit", "--project", "--dir", ".agents"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			if err := run(context.Background(), append([]string{"agent"}, args...), &out, &stderr); err == nil {
				t.Fatal("expected invalid command error")
			}
			if out.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("errors must be returned, not printed: stdout=%q stderr=%q", &out, &stderr)
			}
		})
	}
}

func TestPRCommentsHelp(t *testing.T) {
	var out, stderr bytes.Buffer
	if err := run(context.Background(), []string{"pr", "comments", "--help"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "gh kit pr comments") || !strings.Contains(out.String(), "--pr") {
		t.Fatalf("unexpected help: %s", &out)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", &stderr)
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
