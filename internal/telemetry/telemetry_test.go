package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

type recordingTransport struct {
	mu     sync.Mutex
	bodies []string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.bodies = append(r.bodies, string(body))
	r.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
}

func (r *recordingTransport) payload() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.bodies, "\n")
}

func testClient(t *testing.T) (context.Context, *sentry.Client, *recordingTransport) {
	t.Helper()
	transport := &recordingTransport{}
	opts := options()
	opts.HTTPTransport = transport
	client, err := sentry.NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	hub := sentry.NewHub(client, sentry.NewScope())
	return sentry.SetHubOnContext(context.Background(), hub), client, transport
}

func TestOptOut(t *testing.T) {
	for _, value := range []string{"true", "TRUE", "True"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TELEMETRY_DISABLED", value)
			ctx, shutdown := Init(context.Background())
			defer shutdown()
			if sentry.GetHubFromContext(ctx) != nil {
				t.Fatal("opt-out initialized Sentry")
			}
			failure := errors.New("command failed")
			if err := RunCommand(ctx, "gh kit agent skills list", func(context.Context) error { return failure }); !errors.Is(err, failure) {
				t.Fatalf("opt-out changed command execution: %v", err)
			}
		})
	}
}

func TestEnabledByDefault(t *testing.T) {
	for _, value := range []string{"", "false", "1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TELEMETRY_DISABLED", value)
			ctx, shutdown := Init(context.Background())
			defer shutdown()
			if sentry.GetHubFromContext(ctx) == nil {
				t.Fatal("telemetry should be enabled")
			}
		})
	}
}

func TestCommandSignals(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failed], func(t *testing.T) {
			ctx, client, transport := testClient(t)
			failure := errors.New("secret-token /private/repository")
			err := RunCommand(ctx, "gh kit agent skills list", func(ctx context.Context) error {
				return TraceRequest(ctx, "GET", func(context.Context) error {
					if failed {
						return failure
					}
					return nil
				})
			})
			if failed && !errors.Is(err, failure) {
				t.Fatalf("lost command error: %v", err)
			}
			if !client.Flush(2 * time.Second) {
				t.Fatal("flush timed out")
			}
			payload := transport.payload()
			for _, want := range []string{"transaction", "console.command", "gh kit agent skills list", "CLI command started", "gh_kit.command.count", "gh_kit.command.duration", "gh_kit.runtime.goroutines", "http.client", "GitHub API request", `"breadcrumbs"`, `"modules"`, `"runtime"`} {
				if !strings.Contains(payload, want) {
					t.Errorf("missing %q in payload: %s", want, payload)
				}
			}
			if failed {
				if strings.Count(payload, `"type":"event"`) != 1 || !strings.Contains(payload, `"status":"internal_error"`) {
					t.Error("missing failure event or failed transaction status")
				}
			} else if strings.Contains(payload, `"type":"event"`) || !strings.Contains(payload, "CLI command completed") || !strings.Contains(payload, `"status":"ok"`) {
				t.Error("successful command reported incorrectly")
			}
			for _, forbidden := range []string{"secret-token", "/private/repository", "sentry.server.address", "server_name"} {
				if strings.Contains(payload, forbidden) {
					t.Errorf("leaked %q", forbidden)
				}
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGitHubRequestTracing(t *testing.T) {
	ctx, client, transport := testClient(t)
	var parentID sentry.SpanID
	requests := 0
	github, err := api.NewRESTClient(api.ClientOptions{
		Host: "github.example.com", AuthToken: "test-token", LogIgnoreEnv: true,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			span := sentry.SpanFromContext(req.Context())
			if span == nil || span.Op != "http.client" || span.ParentSpanID != parentID {
				t.Fatal("request is not a child of the command transaction")
			}
			if req.Header.Get("sentry-trace") != "" || req.Header.Get("baggage") != "" {
				t.Fatal("tracing headers sent to GitHub")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("[]")), Request: req}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = RunCommand(ctx, "gh kit pr comments", func(ctx context.Context) error {
		parentID = sentry.SpanFromContext(ctx).SpanID
		return TraceRequest(ctx, "GET", func(ctx context.Context) error {
			var comments []any
			return github.DoWithContext(ctx, "GET", "repos/private-value/private-value/pulls/42/comments?private-value=secret-value", nil, &comments)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d, want 1", requests)
	}
	if !client.Flush(2 * time.Second) {
		t.Fatal("flush timed out")
	}
	payload := transport.payload()
	if !strings.Contains(payload, `"op":"http.client"`) {
		t.Fatalf("missing request span: %s", payload)
	}
	for _, private := range []string{"private-value", "secret-value", "test-token", "github.example.com"} {
		if strings.Contains(payload, private) {
			t.Errorf("request data leaked: %s", private)
		}
	}
}

func TestCommandPanic(t *testing.T) {
	for _, value := range []any{"private-value", errors.New("private-value")} {
		t.Run("panic", func(t *testing.T) {
			ctx, client, transport := testClient(t)
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_ = RunCommand(ctx, "gh kit agent skills list", func(context.Context) error { panic(value) })
			}()
			if recovered != value {
				t.Fatalf("panic changed: %v", recovered)
			}
			if !client.Flush(2 * time.Second) {
				t.Fatal("flush timed out")
			}
			payload := transport.payload()
			if strings.Count(payload, `"type":"event"`) != 1 {
				t.Fatalf("expected one panic event: %s", payload)
			}
			if !strings.Contains(payload, `"status":"internal_error"`) || !strings.Contains(payload, `"frames"`) {
				t.Fatalf("missing panic trace: %s", payload)
			}
			if strings.Contains(payload, "private-value") {
				t.Fatal("panic content leaked")
			}
		})
	}
}

func TestPrivacyFilters(t *testing.T) {
	ctx, client, transport := testClient(t)
	hub := sentry.GetHubFromContext(ctx)
	hub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetUser(sentry.User{Email: "private-value"})
		scope.SetTag("private", "private-value")
		scope.SetContext("private", sentry.Context{"data": "private-value"})
		scope.AddBreadcrumb(&sentry.Breadcrumb{Message: "private-value"}, 10)
	})
	event := sentry.NewEvent()
	event.Message = "private-value"
	event.Request = &sentry.Request{URL: "https://private-value.invalid"}
	event.Exception = []sentry.Exception{{Type: "error", Value: "private-value", Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{{AbsPath: "/private-value/comments.go", Module: "github.com/philprime/gh-kit/cmd/pr", Function: "run", Vars: map[string]interface{}{"secret": "private-value"}, ContextLine: "private-value"}}}}}
	hub.CaptureEvent(event)
	logger := sentry.NewLogger(ctx)
	logger.Info().String("private", "private-value").Emit("CLI command started")
	logger.Info().Emit("private-value")
	meter := sentry.NewMeter(ctx)
	meter.Count("gh_kit.command.count", 1, sentry.WithAttributes(attribute.String("private", "private-value")))
	meter.Count("private-value", 1)
	if !client.Flush(2 * time.Second) {
		t.Fatal("flush timed out")
	}
	payload := transport.payload()
	if strings.Contains(payload, "private-value") {
		t.Fatalf("private data escaped: %s", payload)
	}
	if !strings.Contains(payload, `"filename":"cmd/pr/comments.go"`) || !strings.Contains(payload, "CLI command failed") {
		t.Fatalf("lost useful error details: %s", payload)
	}
}
