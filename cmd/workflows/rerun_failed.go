package workflows

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/philprime/gh-kit/internal/repoflag"
	"github.com/philprime/gh-kit/internal/telemetry"
	"github.com/spf13/cobra"
)

func newRerunCmd(deps dependencies) *cobra.Command {
	var pr int
	var failed, cancelled bool
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
			repo, err := repoflag.Resolve(command, deps.currentRepository)
			if err != nil {
				return err
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
	repoflag.Add(command)
	return command
}

func rerunWorkflows(ctx context.Context, client *api.RESTClient, repo string, pr int, failed, cancelled bool, out io.Writer) error {
	base := "repos/" + repo
	fmt.Fprintf(out, "Fetching PR #%d in %s.\n", pr, repo)
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
		fmt.Fprintf(out, "Listing workflow runs for head %s (page %d).\n", pull.Head.SHA, page)
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
	fmt.Fprintf(out, "Found %d completed workflow run(s) for PR #%d's current head commit.\n", len(runs), pr)
	// Preflight every job page before issuing any rerun requests.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pending := make(chan int, len(runs))
	for index := range runs {
		pending <- index
	}
	close(pending)
	jobsByRun := make([][]int64, len(runs))
	var workers sync.WaitGroup
	var outputMu sync.Mutex
	var firstError sync.Once
	var inspectionErr error
	for range min(5, len(runs)) {
		workers.Go(func() {
			for index := range pending {
				if ctx.Err() != nil {
					return
				}
				run := runs[index]
				jobs, err := matchingJobsForRun(ctx, client, base, run.ID, failed, cancelled, func(page int) {
					outputMu.Lock()
					defer outputMu.Unlock()
					fmt.Fprintf(out, "Inspecting jobs for workflow run %d (%d/%d, page %d).\n", run.ID, index+1, len(runs), page)
				})
				if err != nil {
					firstError.Do(func() {
						inspectionErr = err
						cancel()
					})
					return
				}
				jobsByRun[index] = jobs
			}
		})
	}
	workers.Wait()
	if inspectionErr != nil {
		return inspectionErr
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inspect workflow jobs: %w", err)
	}
	// Keep rerun requests in discovery order regardless of inspection completion order.
	var jobs []int64
	for _, runJobs := range jobsByRun {
		jobs = append(jobs, runJobs...)
	}
	fmt.Fprintf(out, "Found %d matching job(s).\n", len(jobs))
	if len(jobs) == 0 {
		fmt.Fprintf(out, "No matching jobs for PR #%d's current head commit.\n", pr)
		return nil
	}
	requested := 0
	var failures []error
	for index, job := range jobs {
		fmt.Fprintf(out, "Requesting rerun for job %d (%d/%d).\n", job, index+1, len(jobs))
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

func matchingJobsForRun(ctx context.Context, client *api.RESTClient, base string, runID int64, failed, cancelled bool, progress func(int)) ([]int64, error) {
	var jobs []int64
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
		progress(page)
		path := fmt.Sprintf("%s/actions/runs/%d/jobs?filter=latest&per_page=100&page=%d", base, runID, page)
		if err := telemetry.TraceRequest(ctx, http.MethodGet, func(ctx context.Context) error {
			return client.DoWithContext(ctx, http.MethodGet, path, nil, &response)
		}); err != nil {
			return nil, fmt.Errorf("list jobs for workflow run %d: %w", runID, err)
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
			return jobs, nil
		}
	}
}
