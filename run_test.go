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
