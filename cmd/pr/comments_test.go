package pr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

type requestContextKey struct{}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestComments(t *testing.T) {
	for _, scenario := range []string{"paginated", "empty", "api error", "invalid json", "output error"} {
		t.Run(scenario, func(t *testing.T) {
			requests := 0
			client, err := api.NewRESTClient(api.ClientOptions{
				Host: "github.example.com", AuthToken: "test-token", LogIgnoreEnv: true,
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					requests++
					if r.Context().Value(requestContextKey{}) != "command" {
						t.Fatal("command context lost")
					}
					if r.Method != "GET" || r.URL.Path != "/api/v3/repos/owner/repo/pulls/42/comments" || r.URL.Query().Get("per_page") != "100" {
						t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
					}
					status, body := 200, `[]`
					switch scenario {
					case "paginated":
						switch r.URL.Query().Get("page") {
						case "1":
							var comments []string
							for i := 0; i < 100; i++ {
								comments = append(comments, `{"path":"a.go","body":"first\nline","id":9007199254740993,"user":{"login":"reviewer"},"line":null}`)
							}
							body = "[" + strings.Join(comments, ",") + "]"
						case "2":
							body = `[{"path":"b.go","body":"last"}]`
						default:
							t.Fatalf("unexpected page: %s", r.URL)
						}
					case "api error":
						status, body = 403, `{"message":"Forbidden"}`
					case "invalid json":
						body = `not json`
					case "output error":
						body = `[{"path":"a.go","body":"comment"}]`
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			command := newCommentsCmd(dependencies{
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
			command.SetOut(&out)
			if scenario == "output error" {
				command.SetOut(errorWriter{})
			}
			command.SetArgs([]string{"--pr", "42"})
			err = command.ExecuteContext(context.WithValue(context.Background(), requestContextKey{}, "command"))
			if strings.Contains(scenario, "error") || scenario == "invalid json" {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "[]\n"
			if scenario == "paginated" {
				want = "[" + strings.Repeat(`{"path":"a.go","body":"first\nline","id":9007199254740993,"user":{"login":"reviewer"},"line":null},`, 100) + "{\"path\":\"b.go\",\"body\":\"last\"}]\n"
				if requests != 2 {
					t.Fatalf("got %d requests, want 2", requests)
				}
			}
			if out.String() != want {
				t.Fatalf("unexpected output: %q", out.String())
			}
		})
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestCommentsSetupErrors(t *testing.T) {
	for _, stage := range []string{"repository", "client"} {
		t.Run(stage, func(t *testing.T) {
			failure := fmt.Errorf("%s failed", stage)
			command := newCommentsCmd(dependencies{
				currentRepository: func() (repository.Repository, error) {
					if stage == "repository" {
						return repository.Repository{}, failure
					}
					return repository.Repository{Host: "github.com", Owner: "owner", Name: "repo"}, nil
				},
				newRESTClient: func(api.ClientOptions) (*api.RESTClient, error) { return nil, failure },
			})
			command.SetArgs([]string{"--pr", "42"})
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			if err := command.Execute(); !errors.Is(err, failure) {
				t.Fatalf("expected wrapped setup error, got %v", err)
			}
		})
	}
}
