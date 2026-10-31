package workflows

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

func TestLogs(t *testing.T) {
	const logs = "2026-01-01T00:00:00Z \x1b[31mfailed\x1b[0m\r\nlast line"
	for _, scenario := range []string{"raw", "redirect", "api error", "output error"} {
		t.Run(scenario, func(t *testing.T) {
			requests := 0
			client, err := api.NewRESTClient(api.ClientOptions{
				Host: "github.example.com", AuthToken: "test-token", LogIgnoreEnv: true,
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					requests++
					if r.Method != "GET" {
						t.Fatalf("unexpected method: %s", r.Method)
					}
					status, body := http.StatusOK, logs
					header := http.Header{"Content-Type": {"text/plain"}}
					if requests == 1 {
						if r.URL.Path != "/api/v3/repos/owner/repo/actions/jobs/124568269207/logs" {
							t.Fatalf("unexpected URL: %s", r.URL)
						}
						if scenario == "redirect" {
							status, body = http.StatusFound, ""
							header.Set("Location", "https://logs.example.net/job.txt")
						}
						if scenario == "api error" {
							status, body = http.StatusForbidden, `{"message":"Forbidden"}`
							header.Set("Content-Type", "application/json")
						}
					} else if scenario != "redirect" || requests != 2 || r.URL.String() != "https://logs.example.net/job.txt" {
						t.Fatalf("unexpected request: %s", r.URL)
					} else if r.Header.Get("Authorization") != "" {
						t.Fatal("credentials forwarded to log storage")
					}
					return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			command := newLogsCmd(dependencies{
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
			command.SetErr(io.Discard)
			if scenario == "output error" {
				command.SetOut(logErrorWriter{})
			}
			command.SetArgs([]string{"--job", "124568269207"})
			err = command.Execute()
			if scenario == "api error" || scenario == "output error" {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != logs {
				t.Fatalf("unexpected logs: %q", out.String())
			}
			if scenario == "redirect" && requests != 2 {
				t.Fatalf("got %d requests, want 2", requests)
			}
		})
	}
}

type logErrorWriter struct{}

func (logErrorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestLogsSetupErrors(t *testing.T) {
	for _, stage := range []string{"repository", "client"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New("setup failed")
			command := newLogsCmd(dependencies{
				currentRepository: func() (repository.Repository, error) {
					if stage == "repository" {
						return repository.Repository{}, failure
					}
					return repository.Repository{Host: "github.com", Owner: "owner", Name: "repo"}, nil
				},
				newRESTClient: func(api.ClientOptions) (*api.RESTClient, error) { return nil, failure },
			})
			command.SetArgs([]string{"--job", "124568269207"})
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			if err := command.Execute(); !errors.Is(err, failure) {
				t.Fatalf("expected wrapped setup error, got %v", err)
			}
		})
	}
}
