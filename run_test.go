package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/philprime/gh-kit/cmd"
)

type telemetryTransport struct {
	mu      sync.Mutex
	payload string
}

func (r *telemetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.payload += string(body)
	r.mu.Unlock()
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
}

func TestRunTelemetry(t *testing.T) {
	for _, tc := range []struct {
		name            string
		args            []string
		writer          io.Writer
		wantError       bool
		wantTransaction bool
	}{
		{"success", []string{"agent", "skills", "list"}, io.Discard, false, true},
		{"failure", []string{"agent", "skills", "list"}, failingWriter{}, true, true},
		{"help", []string{"agent", "skills", "list", "--help"}, io.Discard, false, false},
		{"invalid", []string{"agent", "skills", "list", "private-value"}, io.Discard, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &telemetryTransport{}
			client, err := sentry.NewClient(sentry.ClientOptions{
				Dsn: "https://public@example.invalid/1", EnableTracing: true, TracesSampleRate: 1,
				HTTPTransport: transport, DisableClientReports: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx := sentry.SetHubOnContext(context.Background(), sentry.NewHub(client, sentry.NewScope()))
			err = run(ctx, tc.args, tc.writer, io.Discard)
			if (err != nil) != tc.wantError {
				t.Fatalf("run error: %v", err)
			}
			if !client.Flush(2 * time.Second) {
				t.Fatal("flush timed out")
			}
			transport.mu.Lock()
			payload := transport.payload
			transport.mu.Unlock()
			if got := strings.Contains(payload, `"transaction":"gh kit agent skills list"`); got != tc.wantTransaction {
				t.Fatalf("transaction=%v, want %v: %s", got, tc.wantTransaction, payload)
			}
			if strings.Contains(payload, "private-value") {
				t.Fatal("argument leaked into telemetry")
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestCommandHelp(t *testing.T) {
	for _, args := range [][]string{
		{"--help"}, {"workflows", "--help"}, {"workflows", "cancel-all", "--help"},
		{"workflows", "logs", "--help"}, {"workflows", "rerun", "--help"},
		{"pr", "--help"}, {"pr", "comments", "--help"},
		{"agent", "--help"}, {"agent", "skills", "--help"},
		{"agent", "skills", "list", "--help"}, {"agent", "skills", "get", "--help"},
		{"agent", "skills", "install", "--help"}, {"agent", "skills", "uninstall", "--help"},
	} {
		var out, stderr bytes.Buffer
		if err := run(context.Background(), args, &out, &stderr); err != nil {
			t.Fatalf("help: %v", err)
		}
		if !strings.Contains(out.String(), "gh kit") || !strings.Contains(out.String(), "Usage:") {
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
			if out.Len() != 0 || !strings.Contains(stderr.String(), "Usage:") {
				t.Fatalf("invalid invocation must show help on stderr: stdout=%q stderr=%q", &out, &stderr)
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
			if out.Len() != 0 || !strings.Contains(stderr.String(), "Usage:") {
				t.Fatalf("invalid invocation must show help on stderr: stdout=%q stderr=%q", &out, &stderr)
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

func TestInvalidFlagsShowHelp(t *testing.T) {
	for _, args := range [][]string{
		{}, {"workflows"}, {"workflows", "cancel-all"}, {"workflows", "logs"}, {"workflows", "rerun"},
		{"pr"}, {"pr", "comments"}, {"agent"}, {"agent", "skills"},
		{"agent", "skills", "list"}, {"agent", "skills", "get"},
		{"agent", "skills", "install"}, {"agent", "skills", "uninstall"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			err := run(context.Background(), append(args, "--unknown"), &out, &stderr)
			if err == nil || !strings.Contains(err.Error(), "unknown flag") {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Len() != 0 || !strings.Contains(stderr.String(), "Usage:") {
				t.Fatalf("missing help: stdout=%q stderr=%q", &out, &stderr)
			}
		})
	}
}

func TestRepositoryFlagsAtEveryLevel(t *testing.T) {
	for _, path := range [][]string{
		{"pr", "comments"}, {"workflows", "logs"},
		{"workflows", "cancel-all"}, {"workflows", "rerun"},
	} {
		for _, flag := range []string{"--repo", "-R"} {
			for position := 0; position <= len(path); position++ {
				args := append([]string{}, path[:position]...)
				args = append(args, flag, "owner/other")
				args = append(args, path[position:]...)
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					root := cmd.NewRootCommand()
					root.SetArgs(args)
					root.SetOut(io.Discard)
					root.SetErr(io.Discard)
					command, err := root.ExecuteC()
					// Missing required flags stop execution before any GitHub requests.
					if err == nil || !strings.Contains(err.Error(), "required flag") {
						t.Fatalf("unexpected error: %v", err)
					}
					value, err := command.Flags().GetString("repo")
					if err != nil || value != "owner/other" || !command.Flags().Changed("repo") {
						t.Fatalf("repository flag did not reach subcommand: value=%q error=%v", value, err)
					}
				})
			}
		}
	}
}

func TestRepositoryHelp(t *testing.T) {
	for _, args := range [][]string{
		{"--help"}, {"pr", "--help"}, {"workflows", "--help"},
		{"pr", "comments", "--help"}, {"workflows", "logs", "--help"},
		{"workflows", "cancel-all", "--help"}, {"workflows", "rerun", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			if err := run(context.Background(), args, &out, &stderr); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "--repo") || !strings.Contains(out.String(), "current repository") {
				t.Fatalf("missing repository selection help: %s", &out)
			}
		})
	}
}

func TestInvalidInvocationShowsRelevantHelp(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		usage string
	}{
		{[]string{"workflows", "logs"}, "gh kit workflows logs [flags]"},
		{[]string{"workflows", "unknown"}, "gh kit workflows [command]"},
		{[]string{"workflows", "rerun", "--pr", "1"}, "gh kit workflows rerun [flags]"},
		{[]string{"pr", "comments", "--unknown"}, "gh kit pr comments [flags]"},
		{[]string{"agent", "skills", "get"}, "gh kit agent skills get <name> [flags]"},
		{[]string{"workflows", "--repo", "owner/repo"}, "gh kit workflows [command]"},
		{[]string{"pr", "--repo", "owner/repo"}, "gh kit pr [command]"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			err := run(context.Background(), tc.args, &out, &stderr)
			if err == nil || strings.Contains(err.Error(), "unknown flag: --repo") {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Len() != 0 || !strings.Contains(stderr.String(), tc.usage) {
				t.Fatalf("wrong help: stdout=%q stderr=%q", &out, &stderr)
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
