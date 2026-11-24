package workflows

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

func TestRerunJobSelection(t *testing.T) {
	for _, tt := range []struct {
		name  string
		flags []string
		want  []string
	}{
		{"failed", []string{"--failed"}, []string{"/repos/owner/repo/actions/jobs/11/rerun", "/repos/owner/repo/actions/jobs/14/rerun"}},
		{"cancelled", []string{"--cancelled"}, []string{"/repos/owner/repo/actions/jobs/12/rerun"}},
		{"both", []string{"--failed", "--cancelled"}, []string{"/repos/owner/repo/actions/jobs/11/rerun", "/repos/owner/repo/actions/jobs/12/rerun", "/repos/owner/repo/actions/jobs/14/rerun"}},
		{"cancelled with failed false", []string{"--failed=false", "--cancelled"}, []string{"/repos/owner/repo/actions/jobs/12/rerun"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var attempted []string
			var mutationMu sync.Mutex
			jobPages := 0
			client := testClient(t, func(r *http.Request) (int, string) {
				switch {
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/pulls/42"):
					return 200, `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/actions/runs"):
					return 200, `{"total_count":1,"workflow_runs":[{"id":1,"status":"completed","conclusion":"cancelled","head_sha":"current","head_repository":{"id":7}}]}`
				case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/actions/runs/1/jobs":
					jobPages++
					q := r.URL.Query()
					if q.Get("per_page") != "100" || q.Get("filter") != "latest" {
						t.Errorf("unexpected query: %s", r.URL.RawQuery)
					}
					switch q.Get("page") {
					case "1":
						return 200, `{"total_count":6,"jobs":[{"id":11,"status":"completed","conclusion":"failure"},{"id":12,"status":"completed","conclusion":"cancelled"},{"id":13,"status":"completed","conclusion":"success"}]}`
					case "2":
						return 200, `{"total_count":6,"jobs":[{"id":14,"status":"completed","conclusion":"timed_out"},{"id":15,"status":"completed","conclusion":"skipped"},{"id":16,"status":"in_progress","conclusion":null}]}`
					}
				case r.Method == "POST":
					if jobPages != 2 {
						t.Error("mutation before all jobs collected")
					}
					mutationMu.Lock()
					attempted = append(attempted, r.URL.Path)
					mutationMu.Unlock()
					return 201, ""
				}
				t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				return 404, `{}`
			})
			command := newRerunCmd(dependencies{
				currentRepository: func() (repository.Repository, error) {
					return repository.Repository{Host: "github.com", Owner: "owner", Name: "repo"}, nil
				},
				newRESTClient: func(api.ClientOptions) (*api.RESTClient, error) { return client, nil },
			})
			command.SetArgs(append([]string{"--pr", "42"}, tt.flags...))
			command.SetOut(io.Discard)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			slices.Sort(attempted)
			if !reflect.DeepEqual(attempted, tt.want) {
				t.Fatalf("attempted %v, want %v", attempted, tt.want)
			}
		})
	}
}

func TestRerunReportsProgressBeforeRequests(t *testing.T) {
	var out bytes.Buffer
	client := testClient(t, func(r *http.Request) (int, string) {
		var progress, body string
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42":
			progress = "Fetching PR #42 in owner/repo."
			body = `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
		case "/repos/owner/repo/actions/runs":
			progress = "Listing workflow runs for head current (page 1)."
			body = `{"total_count":1,"workflow_runs":[{"id":1,"status":"completed","head_sha":"current","head_repository":{"id":7}}]}`
		case "/repos/owner/repo/actions/runs/1/jobs":
			progress = "Inspecting jobs for workflow run 1 (1/1, page 1)."
			body = `{"total_count":1,"jobs":[{"id":12,"status":"completed","conclusion":"cancelled"}]}`
		case "/repos/owner/repo/actions/jobs/12/rerun":
			progress = "Requesting rerun for job 12 (1/1)."
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		if !strings.Contains(out.String(), progress+"\n") {
			t.Errorf("missing progress %q before request %s: %s", progress, r.URL.Path, &out)
		}
		if r.Method == http.MethodPost {
			return http.StatusCreated, ""
		}
		return http.StatusOK, body
	})
	if err := rerunWorkflows(context.Background(), client, "owner/repo", 42, false, true, &out); err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{
		"Found 1 completed workflow run(s) for PR #42's current head commit.",
		"Found 1 matching job(s).",
		"Rerun requested for job 12.",
		"Requested reruns for 1 job(s) for PR #42.",
	} {
		if !strings.Contains(out.String(), message+"\n") {
			t.Errorf("missing output %q: %s", message, &out)
		}
	}
}

func TestRerunNoMatchingJobs(t *testing.T) {
	client := testClient(t, func(r *http.Request) (int, string) {
		if r.Method != "GET" {
			t.Fatalf("unexpected mutation: %s", r.URL)
		}
		if strings.HasSuffix(r.URL.Path, "/pulls/42") {
			return 200, `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
		}
		if strings.HasSuffix(r.URL.Path, "/jobs") {
			return 200, `{"total_count":1,"jobs":[{"id":1,"status":"completed","conclusion":"cancelled"}]}`
		}
		return 200, `{"total_count":1,"workflow_runs":[{"id":1,"status":"completed","conclusion":"cancelled","head_sha":"current","head_repository":{"id":7}}]}`
	})
	var out bytes.Buffer
	if err := rerunWorkflows(context.Background(), client, "owner/repo", 42, true, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No matching jobs") {
		t.Fatalf("unexpected output: %s", &out)
	}
}
