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

func newRerunCmd(deps dependencies) *cobra.Command {
	var pr int
	var failed, cancelled bool
	var repoOverride string
	command := &cobra.Command{
		Use:     "rerun",
		Short:   "Rerun failed or cancelled jobs for a pull request's current head commit",
		Long:    "Request reruns of failed or cancelled jobs on a pull request's current head commit. At least one of --failed or --cancelled must be enabled. Use both to select either conclusion. GitHub also reruns dependent jobs. Active workflow runs are skipped.",
		Example: "  gh kit workflows rerun --failed --pr 123",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !failed && !cancelled {
				return errors.New("at least one of --failed or --cancelled must be enabled")
			}
			if pr <= 0 {
				return errors.New("--pr must be a positive pull request number")
			}
			var repo repository.Repository
			var err error
			if command.Flags().Changed("repo") {
				repo, err = repository.Parse(repoOverride)
				if err != nil {
					return fmt.Errorf("parse repository: %w", err)
				}
			} else {
				repo, err = deps.currentRepository()
				if err != nil {
					return fmt.Errorf("resolve current repository: %w", err)
				}
			}
			client, err := deps.newRESTClient(api.ClientOptions{Host: repo.Host})
			if err != nil {
				return fmt.Errorf("create GitHub client: %w", err)
			}
			return rerunWorkflows(command.Context(), client, repo.Owner+"/"+repo.Name, pr, failed, cancelled, command.OutOrStdout())
		},
	}
	command.Flags().IntVar(&pr, "pr", 0, "Pull request number in the selected repository (required)")
	command.MarkFlagRequired("pr")
	command.Flags().BoolVar(&failed, "failed", false, "Rerun failed or timed-out jobs and their dependents")
	command.Flags().BoolVar(&cancelled, "cancelled", false, "Rerun cancelled jobs and their dependents")
	command.Flags().StringVarP(&repoOverride, "repo", "R", "", "Select another repository using [HOST/]OWNER/REPO or a repository URL")
	return command
}

func rerunWorkflows(ctx context.Context, client *api.RESTClient, repo string, pr int, failed, cancelled bool, out io.Writer) error {
	base := "repos/" + repo
	var pull struct {
		Head struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
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
	// Collect before requesting reruns so mutations cannot change pagination.
	for page := 1; ; page++ {
		var response struct {
			TotalCount int           `json:"total_count"`
			Runs       []workflowRun `json:"workflow_runs"`
		}
		path := fmt.Sprintf("%s/actions/runs?branch=%s&head_sha=%s&per_page=100&page=%d", base, url.QueryEscape(pull.Head.Ref), url.QueryEscape(pull.Head.SHA), page)
		if err := telemetry.TraceRequest(ctx, http.MethodGet, func(ctx context.Context) error {
			return client.DoWithContext(ctx, http.MethodGet, path, nil, &response)
		}); err != nil {
			return fmt.Errorf("list workflow runs: %w", err)
		}
		for _, run := range response.Runs {
			if run.HeadRepository.ID != pull.Head.Repo.ID || run.HeadSHA != pull.Head.SHA || run.Status != "completed" {
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
	// Preflight every job page before issuing any rerun requests.
	var jobs []int64
	for _, run := range runs {
		seen := 0
		for page := 1; ; page++ {
			var response struct {
				TotalCount int `json:"total_count"`
				Jobs       []struct {
					ID         int64  `json:"id"`
					Status     string `json:"status"`
					Conclusion string `json:"conclusion"`
				} `json:"jobs"`
			}
			path := fmt.Sprintf("%s/actions/runs/%d/jobs?filter=latest&per_page=100&page=%d", base, run.ID, page)
			if err := telemetry.TraceRequest(ctx, http.MethodGet, func(ctx context.Context) error {
				return client.DoWithContext(ctx, http.MethodGet, path, nil, &response)
			}); err != nil {
				return fmt.Errorf("list jobs for workflow run %d: %w", run.ID, err)
			}
			for _, job := range response.Jobs {
				if job.Status != "completed" {
					continue
				}
				if (failed && (job.Conclusion == "failure" || job.Conclusion == "timed_out")) || (cancelled && job.Conclusion == "cancelled") {
					jobs = append(jobs, job.ID)
				}
			}
			seen += len(response.Jobs)
			if len(response.Jobs) == 0 || seen >= response.TotalCount {
				break
			}
		}
	}
	if len(jobs) == 0 {
		fmt.Fprintf(out, "No matching jobs for PR #%d's current head commit.\n", pr)
		return nil
	}
	requested := 0
	var failures []error
	for _, job := range jobs {
		var response *http.Response
		err := telemetry.TraceRequest(ctx, http.MethodPost, func(ctx context.Context) error {
			var err error
			response, err = client.RequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/actions/jobs/%d/rerun", base, job), nil)
			return err
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("rerun job %d: %w", job, err))
			continue
		}
		response.Body.Close()
		requested++
		fmt.Fprintf(out, "Rerun requested for job %d.\n", job)
	}
	fmt.Fprintf(out, "Requested reruns for %d job(s) for PR #%d.\n", requested, pr)
	return errors.Join(failures...)
}
