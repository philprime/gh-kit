package workflows

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/philprime/gh-kit/internal/telemetry"
	"github.com/spf13/cobra"
)

type dependencies struct {
	currentRepository func() (repository.Repository, error)
	newRESTClient     func(api.ClientOptions) (*api.RESTClient, error)
}

func newCancelAllCmd(deps dependencies) *cobra.Command {
	var pr int
	command := &cobra.Command{
		Use:     "cancel-all",
		Short:   "Cancel active workflow runs for a pull request's source branch",
		Example: "  gh kit workflows cancel-all --pr 123",
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
			return cancelAllWorkflows(command.Context(), client, repo.Owner+"/"+repo.Name, pr, command.OutOrStdout())
		},
	}
	command.Flags().IntVar(&pr, "pr", 0, "Pull request number in the current repository (required)")
	command.MarkFlagRequired("pr")
	return command
}

type workflowRun struct {
	ID             int64  `json:"id"`
	Status         string `json:"status"`
	HeadRepository struct {
		ID int64 `json:"id"`
	} `json:"head_repository"`
	PullRequests []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}

func cancelAllWorkflows(ctx context.Context, client *api.RESTClient, repo string, pr int, out io.Writer) error {
	base := "repos/" + repo
	var pull struct {
		Head struct {
			Ref  string `json:"ref"`
			Repo *struct {
				ID int64 `json:"id"`
			} `json:"repo"`
		} `json:"head"`
	}
	if err := telemetry.TraceRequest(ctx, http.MethodGet, func(ctx context.Context) error {
		return client.DoWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/pulls/%d", base, pr), nil, &pull)
	}); err != nil {
		return fmt.Errorf("get PR #%d: %w", pr, err)
	}
	if pull.Head.Repo == nil {
		return fmt.Errorf("PR #%d source repository is unavailable", pr)
	}

	var runs []workflowRun
	seen := 0
	// Collect before cancelling so cancellations cannot change the pages being read.
	for page := 1; ; page++ {
		var response struct {
			TotalCount int           `json:"total_count"`
			Runs       []workflowRun `json:"workflow_runs"`
		}
		path := fmt.Sprintf("%s/actions/runs?branch=%s&per_page=100&page=%d", base, url.QueryEscape(pull.Head.Ref), page)
		if err := telemetry.TraceRequest(ctx, http.MethodGet, func(ctx context.Context) error {
			return client.DoWithContext(ctx, http.MethodGet, path, nil, &response)
		}); err != nil {
			return fmt.Errorf("list workflow runs: %w", err)
		}
		for _, run := range response.Runs {
			if run.HeadRepository.ID != pull.Head.Repo.ID {
				continue
			}
			switch run.Status {
			case "queued", "in_progress", "waiting", "pending", "requested":
			default:
				continue
			}
			// Push-triggered and some fork runs have no PR association in this response.
			matches := len(run.PullRequests) == 0
			for _, associated := range run.PullRequests {
				if associated.Number == pr {
					matches = true
				}
			}
			if matches {
				runs = append(runs, run)
			}
		}
		seen += len(response.Runs)
		if len(response.Runs) == 0 || seen >= response.TotalCount {
			break
		}
	}
	if len(runs) == 0 {
		fmt.Fprintf(out, "No active workflow runs for PR #%d.\n", pr)
		return nil
	}

	cancelled := 0
	var failures []error
	for _, run := range runs {
		var response *http.Response
		err := telemetry.TraceRequest(ctx, http.MethodPost, func(ctx context.Context) error {
			var err error
			response, err = client.RequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/actions/runs/%d/cancel", base, run.ID), nil)
			return err
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("cancel workflow run %d: %w", run.ID, err))
			continue
		}
		response.Body.Close()
		cancelled++
		fmt.Fprintf(out, "Cancellation requested for workflow run %d.\n", run.ID)
	}
	fmt.Fprintf(out, "Requested cancellation of %d workflow run(s) for PR #%d.\n", cancelled, pr)
	return errors.Join(failures...)
}
