package workflows

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

type requestContextKey struct{}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(t *testing.T, handler func(*http.Request) (int, string)) *api.RESTClient {
	t.Helper()
	client, err := api.NewRESTClient(api.ClientOptions{
		Host: "github.com", AuthToken: "test-token", LogIgnoreEnv: true,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			status, body := handler(r)
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestCancelAllWorkflows(t *testing.T) {
	// Catch missing pagination and cancellation of completed or unrelated runs.
	var cancelled []string
	client := testClient(t, func(r *http.Request) (int, string) {
		if r.Context().Value(requestContextKey{}) != "command" {
			t.Fatal("command context lost")
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls/42":
			return 200, `{"head":{"ref":"feature/topic","repo":{"id":7}}}`
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/actions/runs":
			if r.URL.Query().Get("branch") != "feature/topic" || r.URL.Query().Get("per_page") != "100" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("page") == "1" {
				return 200, `{"total_count":7,"workflow_runs":[
     {"id":1,"status":"in_progress","head_repository":{"id":7},"pull_requests":[{"number":42}]},
     {"id":2,"status":"queued","head_repository":{"id":7},"pull_requests":[]},
     {"id":3,"status":"completed","head_repository":{"id":7},"pull_requests":[{"number":42}]},
     {"id":4,"status":"in_progress","head_repository":{"id":8},"pull_requests":[{"number":42}]},
     {"id":5,"status":"waiting","head_repository":{"id":7},"pull_requests":[{"number":43}]}]}`
			}
			if r.URL.Query().Get("page") == "2" {
				return 200, `{"total_count":7,"workflow_runs":[
     {"id":6,"status":"waiting","head_repository":{"id":7},"pull_requests":[{"number":42}]},
     {"id":7,"status":"pending","head_repository":{"id":7},"pull_requests":[{"number":42}]}]}`
			}
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/cancel"):
			cancelled = append(cancelled, r.URL.Path)
			return 202, ""
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		return 404, `{}`
	})
	var out bytes.Buffer
	command := newCancelAllCmd(dependencies{
		currentRepository: func() (repository.Repository, error) {
			return repository.Repository{Host: "github.com", Owner: "owner", Name: "repo"}, nil
		},
		newRESTClient: func(options api.ClientOptions) (*api.RESTClient, error) {
			if options.Host != "github.com" {
				t.Fatalf("unexpected host: %s", options.Host)
			}
			return client, nil
		},
	})
	command.SetArgs([]string{"--pr", "42"})
	command.SetOut(&out)
	if err := command.ExecuteContext(context.WithValue(context.Background(), requestContextKey{}, "command")); err != nil {
		t.Fatal(err)
	}
	want := []string{"/repos/owner/repo/actions/runs/1/cancel", "/repos/owner/repo/actions/runs/2/cancel", "/repos/owner/repo/actions/runs/6/cancel", "/repos/owner/repo/actions/runs/7/cancel"}
	if !reflect.DeepEqual(cancelled, want) {
		t.Fatalf("cancelled %v, want %v", cancelled, want)
	}
	if !strings.Contains(out.String(), "4") {
		t.Fatalf("missing cancellation summary: %s", &out)
	}
}

func TestCancelAllWorkflowsNoRuns(t *testing.T) {
	client := testClient(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/pulls/42") {
			return 200, `{"head":{"ref":"topic","repo":{"id":7}}}`
		}
		if r.Method != "GET" {
			t.Errorf("unexpected cancellation: %s", r.URL)
		}
		return 200, `{"total_count":0,"workflow_runs":[]}`
	})
	var out bytes.Buffer
	if err := cancelAllWorkflows(context.Background(), client, "owner/repo", 42, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No active workflow runs") {
		t.Fatalf("unexpected output: %s", &out)
	}
}

func TestCancelAllWorkflowsErrors(t *testing.T) {
	for _, stage := range []string{"pull", "list", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			client := testClient(t, func(r *http.Request) (int, string) {
				if strings.HasSuffix(r.URL.Path, "/pulls/42") {
					if stage == "pull" {
						return 404, `{"message":"Not Found"}`
					}
					return 200, `{"head":{"ref":"topic","repo":{"id":7}}}`
				}
				if r.Method == "GET" {
					if stage == "list" {
						return 403, `{"message":"Forbidden"}`
					}
					return 200, `{"total_count":1,"workflow_runs":[{"id":1,"status":"queued","head_repository":{"id":7}}]}`
				}
				return 403, `{"message":"Forbidden"}`
			})
			if err := cancelAllWorkflows(context.Background(), client, "owner/repo", 42, io.Discard); err == nil {
				t.Fatal("expected API error")
			}
		})
	}
}

func TestCancelAllWorkflowsContinuesAfterFailure(t *testing.T) {
	var attempted []string
	client := testClient(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/pulls/42") {
			return 200, `{"head":{"ref":"topic","repo":{"id":7}}}`
		}
		if r.Method == "GET" {
			return 200, `{"total_count":2,"workflow_runs":[{"id":1,"status":"queued","head_repository":{"id":7}},{"id":2,"status":"in_progress","head_repository":{"id":7}}]}`
		}
		attempted = append(attempted, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/1/cancel") {
			return 403, `{"message":"Forbidden"}`
		}
		return 202, ""
	})
	var out bytes.Buffer
	err := cancelAllWorkflows(context.Background(), client, "owner/repo", 42, &out)
	if err == nil || !strings.Contains(err.Error(), "run 1") {
		t.Fatalf("expected run 1 error, got %v", err)
	}
	if !reflect.DeepEqual(attempted, []string{"/repos/owner/repo/actions/runs/1/cancel", "/repos/owner/repo/actions/runs/2/cancel"}) {
		t.Fatalf("unexpected attempts: %v", attempted)
	}
	if !strings.Contains(out.String(), "Requested cancellation of 1 workflow run(s)") {
		t.Fatalf("incorrect summary: %s", &out)
	}
}

func TestCancelAllWorkflowsUnavailableSource(t *testing.T) {
	client := testClient(t, func(r *http.Request) (int, string) {
		if !strings.HasSuffix(r.URL.Path, "/pulls/42") {
			t.Errorf("unexpected request: %s", r.URL)
		}
		return 200, `{"head":{"ref":"topic","repo":null}}`
	})
	if err := cancelAllWorkflows(context.Background(), client, "owner/repo", 42, io.Discard); err == nil {
		t.Fatal("expected unavailable source error")
	}
}

func TestCancelAllCommandPullRequest(t *testing.T) {
	for _, pr := range []string{"1", "42"} {
		t.Run(pr, func(t *testing.T) {
			client := testClient(t, func(r *http.Request) (int, string) {
				switch r.URL.Path {
				case "/repos/owner/repo/pulls/" + pr:
					return 200, `{"head":{"ref":"topic","repo":{"id":7}}}`
				case "/repos/owner/repo/actions/runs":
					return 200, `{"total_count":0,"workflow_runs":[]}`
				default:
					t.Errorf("unexpected request: %s", r.URL)
					return 404, `{}`
				}
			})
			command := newCancelAllCmd(dependencies{
				currentRepository: func() (repository.Repository, error) {
					return repository.Repository{Host: "github.example.com", Owner: "owner", Name: "repo"}, nil
				},
				newRESTClient: func(options api.ClientOptions) (*api.RESTClient, error) {
					if options.Host != "github.example.com" {
						t.Fatalf("unexpected host: %s", options.Host)
					}
					return client, nil
				},
			})
			var out bytes.Buffer
			command.SetArgs([]string{"--pr", pr})
			command.SetOut(&out)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "PR #"+pr) {
				t.Fatalf("unexpected output: %s", &out)
			}
		})
	}
}
