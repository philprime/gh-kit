package workflows

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

func TestRerunFailedCommand(t *testing.T) {
	var attempted []string
	pages := 0
	client := testClient(t, func(r *http.Request) (int, string) {
		if r.Context().Value(requestContextKey{}) != "command" {
			t.Fatal("command context lost")
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls/42":
			return 200, `{"head":{"ref":"feature/topic","sha":"current","repo":{"id":7}}}`
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/actions/runs":
			pages++
			q := r.URL.Query()
			if q.Get("branch") != "feature/topic" || q.Get("head_sha") != "current" || q.Get("per_page") != "100" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			switch q.Get("page") {
			case "1":
				return 200, `{"total_count":8,"workflow_runs":[
     {"id":1,"status":"completed","conclusion":"failure","head_sha":"current","head_repository":{"id":7},"pull_requests":[{"number":42}]},
     {"id":2,"status":"completed","conclusion":"success","head_sha":"current","head_repository":{"id":7}},
     {"id":3,"status":"in_progress","conclusion":"failure","head_sha":"current","head_repository":{"id":7}},
     {"id":4,"status":"completed","conclusion":"failure","head_sha":"current","head_repository":{"id":8}},
     {"id":5,"status":"completed","conclusion":"failure","head_sha":"current","head_repository":{"id":7},"pull_requests":[{"number":43}]},
     {"id":6,"status":"completed","conclusion":"cancelled","head_sha":"current","head_repository":{"id":7}}]}`
			case "2":
				return 200, `{"total_count":8,"workflow_runs":[
     {"id":7,"status":"completed","conclusion":"timed_out","head_sha":"current","head_repository":{"id":7},"pull_requests":[]},
     {"id":8,"status":"completed","conclusion":"failure","head_sha":"old","head_repository":{"id":7}}]}`
			}
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/actions/runs/"):
			id := strings.Split(r.URL.Path, "/")[6]
			conclusion := "failure"
			if id == "2" {
				conclusion = "success"
			}
			if id == "6" {
				conclusion = "cancelled"
			}
			if id == "7" {
				conclusion = "timed_out"
			}
			return 200, fmt.Sprintf(`{"total_count":1,"jobs":[{"id":%s,"status":"completed","conclusion":%q}]}`, id, conclusion)
		case r.Method == "POST":
			if pages != 2 {
				t.Error("rerun requested before collecting all pages")
			}
			attempted = append(attempted, r.URL.Path)
			return 201, ""
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		return 404, `{}`
	})
	command := newRerunCmd(dependencies{
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
	command.SetArgs([]string{"--pr", "42", "--failed"})
	command.SetOut(&out)
	if err := command.ExecuteContext(context.WithValue(context.Background(), requestContextKey{}, "command")); err != nil {
		t.Fatal(err)
	}
	want := []string{"/repos/owner/repo/actions/jobs/1/rerun", "/repos/owner/repo/actions/jobs/7/rerun"}
	if !reflect.DeepEqual(attempted, want) {
		t.Fatalf("attempted %v, want %v", attempted, want)
	}
	if !strings.Contains(out.String(), "Requested reruns for 2 job(s) for PR #42") {
		t.Fatalf("unexpected summary: %s", &out)
	}
}

func TestRerunFailedNoRuns(t *testing.T) {
	client := testClient(t, func(r *http.Request) (int, string) {
		if r.Method != "GET" {
			t.Fatalf("unexpected mutation: %s", r.URL)
		}
		if strings.HasSuffix(r.URL.Path, "/pulls/42") {
			return 200, `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
		}
		return 200, `{"total_count":0,"workflow_runs":[]}`
	})
	var out bytes.Buffer
	if err := rerunWorkflows(context.Background(), client, "owner/repo", 42, true, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No matching jobs") {
		t.Fatalf("unexpected output: %s", &out)
	}
}

func TestRerunFailedErrors(t *testing.T) {
	for _, stage := range []string{"pull", "source", "list", "jobs", "rerun"} {
		t.Run(stage, func(t *testing.T) {
			var attempted []string
			client := testClient(t, func(r *http.Request) (int, string) {
				if strings.HasSuffix(r.URL.Path, "/pulls/42") {
					if stage == "pull" {
						return 404, `{"message":"Not Found"}`
					}
					if stage == "source" {
						return 200, `{"head":{"ref":"topic","sha":"current","repo":null}}`
					}
					return 200, `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
				}
				if r.Method == "GET" && strings.Contains(r.URL.Path, "/actions/runs/") {
					if stage == "jobs" {
						return 403, `{"message":"Forbidden"}`
					}
					id := strings.Split(r.URL.Path, "/")[6]
					return 200, fmt.Sprintf(`{"total_count":1,"jobs":[{"id":%s,"status":"completed","conclusion":"failure"}]}`, id)
				}
				if r.Method == "GET" {
					if stage == "list" {
						return 403, `{"message":"Forbidden"}`
					}
					return 200, `{"total_count":2,"workflow_runs":[{"id":1,"status":"completed","conclusion":"failure","head_sha":"current","head_repository":{"id":7}},{"id":2,"status":"completed","conclusion":"failure","head_sha":"current","head_repository":{"id":7}}]}`
				}
				attempted = append(attempted, r.URL.Path)
				if strings.Contains(r.URL.Path, "/1/") {
					return 403, `{"message":"Forbidden"}`
				}
				return 201, ""
			})
			var out bytes.Buffer
			err := rerunWorkflows(context.Background(), client, "owner/repo", 42, true, false, &out)
			if err == nil {
				t.Fatal("expected error")
			}
			if stage == "rerun" {
				if !strings.Contains(err.Error(), "job 1") || len(attempted) != 2 || !strings.Contains(out.String(), "Requested reruns for 1 job(s)") {
					t.Fatalf("unexpected partial result: %v, %v, %s", err, attempted, &out)
				}
			} else if len(attempted) != 0 {
				t.Fatalf("unexpected mutations: %v", attempted)
			}
		})
	}
}

func TestRerunFailedCommandValidation(t *testing.T) {
	for _, args := range [][]string{nil, {"--pr", "42"}, {"--pr", "42", "--failed=false"}, {"--pr", "42", "--cancelled=false"}, {"--pr", "42", "--failed=false", "--cancelled=false"}, {"--pr", "0", "--failed"}, {"--pr", "-1", "--failed"}, {"--pr", "42", "--failed", "extra"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			command := newRerunCmd(dependencies{currentRepository: func() (repository.Repository, error) {
				t.Fatal("repository accessed for invalid arguments")
				return repository.Repository{}, nil
			}})
			command.SetArgs(args)
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			if err := command.Execute(); err == nil {
				t.Fatal("expected argument error")
			}
		})
	}
}
