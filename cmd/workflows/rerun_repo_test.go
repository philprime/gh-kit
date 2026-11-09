package workflows

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

func TestRerunRepositoryOverride(t *testing.T) {
	for _, flag := range []string{"--repo", "-R"} {
		for _, target := range []string{"getsentry/sentry-cocoa", "github.example.com/getsentry/sentry-cocoa", "https://github.example.com/getsentry/sentry-cocoa"} {
			t.Run(flag+target, func(t *testing.T) {
				host := "github.com"
				if strings.Contains(target, "github.example.com") {
					host = "github.example.com"
				}
				client := testClient(t, func(r *http.Request) (int, string) {
					if r.Method != "GET" {
						t.Fatalf("unexpected mutation: %s", r.URL)
					}
					switch r.URL.Path {
					case "/repos/getsentry/sentry-cocoa/pulls/9458":
						return 200, `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
					case "/repos/getsentry/sentry-cocoa/actions/runs":
						return 200, `{"total_count":0,"workflow_runs":[]}`
					default:
						t.Errorf("unexpected request: %s", r.URL)
						return 404, `{}`
					}
				})
				command := newRerunCmd(dependencies{
					currentRepository: func() (repository.Repository, error) {
						t.Fatal("override must not resolve checkout")
						return repository.Repository{}, nil
					},
					newRESTClient: func(options api.ClientOptions) (*api.RESTClient, error) {
						if options.Host != host {
							t.Fatalf("host %s, want %s", options.Host, host)
						}
						return client, nil
					},
				})
				command.SetArgs([]string{"--failed", "--pr", "9458", flag, target})
				command.SetOut(io.Discard)
				if err := command.Execute(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRerunInvalidRepository(t *testing.T) {
	for _, target := range []string{"invalid", ""} {
		t.Run(target, func(t *testing.T) {
			command := newRerunCmd(dependencies{
				currentRepository: func() (repository.Repository, error) {
					t.Fatal("invalid override must not resolve checkout")
					return repository.Repository{}, nil
				},
				newRESTClient: func(api.ClientOptions) (*api.RESTClient, error) {
					t.Fatal("invalid override must not create client")
					return nil, nil
				},
			})
			command.SetArgs([]string{"--failed", "--pr", "9458", "--repo", target})
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			if err := command.Execute(); err == nil {
				t.Fatal("expected invalid repository error")
			}
		})
	}
}
