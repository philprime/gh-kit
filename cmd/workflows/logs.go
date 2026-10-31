package workflows

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/philprime/gh-kit/internal/telemetry"
	"github.com/spf13/cobra"
)

func newLogsCmd(deps dependencies) *cobra.Command {
	var job int64
	command := &cobra.Command{
		Use:     "logs",
		Short:   "Read raw GitHub Actions job logs",
		Long:    "Read logs for a GitHub Actions job in the current repository.\nOutput preserves ANSI escape sequences.",
		Example: "  gh kit workflows logs --job 124568269207",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if job <= 0 {
				return errors.New("--job must be a positive job ID")
			}
			repo, err := deps.currentRepository()
			if err != nil {
				return fmt.Errorf("resolve current repository: %w", err)
			}
			client, err := deps.newRESTClient(api.ClientOptions{Host: repo.Host})
			if err != nil {
				return fmt.Errorf("create GitHub client: %w", err)
			}
			path := fmt.Sprintf("repos/%s/%s/actions/jobs/%d/logs", repo.Owner, repo.Name, job)
			var response *http.Response
			err = telemetry.TraceRequest(command.Context(), http.MethodGet, func(ctx context.Context) error {
				var err error
				response, err = client.RequestWithContext(ctx, http.MethodGet, path, nil)
				return err
			})
			if err != nil {
				return fmt.Errorf("get logs for job %d: %w", job, err)
			}
			defer response.Body.Close()
			if _, err := io.Copy(command.OutOrStdout(), response.Body); err != nil {
				return fmt.Errorf("stream logs for job %d: %w", job, err)
			}
			return nil
		},
	}
	command.Flags().Int64Var(&job, "job", 0, "Job ID in the current repository (required)")
	command.MarkFlagRequired("job")
	return command
}
