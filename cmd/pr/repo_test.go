package pr

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

func TestCommentsRepositoryFlags(t *testing.T) {
	for _, flag := range []string{"--repo", "-R"} {
		for _, target := range []string{"owner/other", "github.example.com/owner/other", "https://github.example.com/owner/other", "invalid", ""} {
			t.Run(flag+target, func(t *testing.T) {
				valid := target != "invalid" && target != ""
				host := "github.com"
				if strings.Contains(target, "github.example.com") {
					host = "github.example.com"
				}
				requests := 0
				command := newCommentsCmd(dependencies{
					currentRepository: func() (repository.Repository, error) {
						t.Fatal("override resolved checkout")
						return repository.Repository{}, nil
					},
					newRESTClient: func(options api.ClientOptions) (*api.RESTClient, error) {
						if !valid {
							t.Fatal("invalid repository created client")
						}
						if options.Host != host {
							t.Fatalf("host %s, want %s", options.Host, host)
						}
						options.AuthToken = "test-token"
						options.LogIgnoreEnv = true
						options.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
							requests++
							path := "/repos/owner/other/pulls/42/comments"
							if host != "github.com" {
								path = "/api/v3" + path
							}
							apiHost := host
							if host == "github.com" {
								apiHost = "api.github.com"
							}
							if r.Method != "GET" || r.URL.Host != apiHost || r.URL.Path != path {
								t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
							}
							return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`[]`)), Request: r}, nil
						})
						return api.NewRESTClient(options)
					},
				})
				command.SetArgs([]string{"--pr", "42", flag, target})
				command.SetOut(io.Discard)
				command.SetErr(io.Discard)
				err := command.Execute()
				if valid && err != nil {
					t.Fatal(err)
				}
				if valid && requests != 1 {
					t.Fatalf("requests %d, want 1", requests)
				}
				if !valid && err == nil {
					t.Fatal("expected invalid repository error")
				}
			})
		}
	}
}
