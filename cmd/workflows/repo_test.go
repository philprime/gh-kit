package workflows

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
)

func TestWorkflowRepositoryFlags(t *testing.T) {
	for _, commandTest := range []struct {
		name       string
		newCommand func(dependencies) *cobra.Command
		args       []string
	}{
		{"logs", newLogsCmd, []string{"--job", "123"}},
		{"cancel-all", newCancelAllCmd, []string{"--pr", "42"}},
		{"rerun", newRerunCmd, []string{"--pr", "42", "--failed"}},
	} {
		for _, flag := range []string{"--repo", "-R"} {
			for _, target := range []string{"owner/other", "github.example.com/owner/other", "https://github.example.com/owner/other", "invalid", ""} {
				t.Run(commandTest.name+flag+target, func(t *testing.T) {
					valid := target != "invalid" && target != ""
					host := "github.com"
					if strings.Contains(target, "github.example.com") {
						host = "github.example.com"
					}
					requests := 0
					deps := dependencies{
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
							return testClient(t, func(r *http.Request) (int, string) {
								requests++
								if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/repos/owner/other/") {
									t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
								}
								switch {
								case strings.HasSuffix(r.URL.Path, "/logs"):
									return 200, "log output"
								case strings.HasSuffix(r.URL.Path, "/pulls/42"):
									return 200, `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
								case strings.HasSuffix(r.URL.Path, "/actions/runs"):
									return 200, `{"total_count":0,"workflow_runs":[]}`
								default:
									t.Fatalf("unexpected path: %s", r.URL.Path)
									return 404, `{}`
								}
							}), nil
						},
					}
					command := commandTest.newCommand(deps)
					command.SetArgs(append(append([]string{}, commandTest.args...), flag, target))
					command.SetOut(io.Discard)
					command.SetErr(io.Discard)
					err := command.Execute()
					if valid && err != nil {
						t.Fatal(err)
					}
					if valid && requests == 0 {
						t.Fatal("no repository request")
					}
					if !valid && err == nil {
						t.Fatal("expected invalid repository error")
					}
				})
			}
		}
	}
}
