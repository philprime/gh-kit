package pr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/philprime/gh-kit/internal/telemetry"
	"github.com/spf13/cobra"
)

type dependencies struct {
	currentRepository func() (repository.Repository, error)
	newRESTClient     func(api.ClientOptions) (*api.RESTClient, error)
}

func newCommentsCmd(deps dependencies) *cobra.Command {
	var pr int
	command := &cobra.Command{
		Use:     "comments",
		Short:   "Read pull request review comments as JSON",
		Long:    "Read all inline review comments, including replies, from a pull request.\nAll pages are combined into a single JSON array with every response field preserved.\nGeneral pull request discussion comments are not included.",
		Example: "  gh kit pr comments --pr 4007",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if pr <= 0 {
				return errors.New("--pr must be a positive pull request number")
			}
			repo, err := deps.currentRepository()
			if err != nil {
				return fmt.Errorf("resolve current repository: %w", err)
			}
			client, err := deps.newRESTClient(api.ClientOptions{Host: repo.Host})
			if err != nil {
				return fmt.Errorf("create GitHub client: %w", err)
			}
			return readComments(command.Context(), client, repo.Owner+"/"+repo.Name, pr, command.OutOrStdout())
		},
	}
	command.Flags().IntVar(&pr, "pr", 0, "Pull request number in the current repository (required)")
	command.MarkFlagRequired("pr")
	return command
}

func readComments(ctx context.Context, client *api.RESTClient, repo string, pr int, out io.Writer) error {
	comments := make([]json.RawMessage, 0)
	for page := 1; ; page++ {
		var response []json.RawMessage
		path := fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100&page=%d", repo, pr, page)
		if err := telemetry.TraceRequest(ctx, http.MethodGet, func(ctx context.Context) error {
			return client.DoWithContext(ctx, http.MethodGet, path, nil, &response)
		}); err != nil {
			return fmt.Errorf("list review comments for PR #%d: %w", pr, err)
		}
		comments = append(comments, response...)
		if len(response) < 100 {
			break
		}
	}
	if err := json.NewEncoder(out).Encode(comments); err != nil {
		return fmt.Errorf("write review comments: %w", err)
	}
	return nil
}
