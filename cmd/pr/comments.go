package pr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/philprime/gh-kit/internal/parallel"
	"github.com/philprime/gh-kit/internal/repoflag"
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
			repo, err := repoflag.Resolve(command, deps.currentRepository)
			if err != nil {
				return err
			}
			client, err := deps.newRESTClient(api.ClientOptions{Host: repo.Host})
			if err != nil {
				return fmt.Errorf("create GitHub client: %w", err)
			}
			return readComments(command.Context(), client, repo.Owner+"/"+repo.Name, pr, command.OutOrStdout())
		},
	}
	command.Flags().IntVar(&pr, "pr", 0, "Pull request number in the selected repository (required)")
	command.MarkFlagRequired("pr")
	repoflag.Add(command)
	return command
}

func readComments(ctx context.Context, client *api.RESTClient, repo string, pr int, out io.Writer) error {
	fetch := func(page int) ([]json.RawMessage, string, error) {
		var response *http.Response
		path := fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100&page=%d", repo, pr, page)
		err := telemetry.TraceRequest(ctx, http.MethodGet, func(ctx context.Context) error {
			var err error
			response, err = client.RequestWithContext(ctx, http.MethodGet, path, nil)
			return err
		})
		if err != nil {
			return nil, "", err
		}
		defer response.Body.Close()
		var comments []json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&comments); err != nil {
			return nil, "", fmt.Errorf("decode comments page %d: %w", page, err)
		}
		return comments, response.Header.Get("Link"), nil
	}
	first, link, err := fetch(1)
	if err != nil {
		return fmt.Errorf("list review comments for PR #%d: %w", pr, err)
	}
	last, err := lastCommentPage(link)
	if err != nil {
		return fmt.Errorf("read comment pagination: %w", err)
	}
	pages := make([][]json.RawMessage, last)
	pages[0] = first
	if last == 1 {
		// Preserve sequential pagination when no last page is advertised.
		for page, size := 2, len(first); size == 100; page++ {
			next, _, err := fetch(page)
			if err != nil {
				return fmt.Errorf("list review comments for PR #%d: %w", pr, err)
			}
			pages = append(pages, next)
			size = len(next)
		}
	} else {
		results := parallel.Do(ctx, last-1, func(index int) error {
			comments, _, err := fetch(index + 2)
			if err != nil {
				return fmt.Errorf("list review comments for PR #%d page %d: %w", pr, index+2, err)
			}
			pages[index+1] = comments
			return nil
		})
		if err := errors.Join(results...); err != nil {
			return err
		}
	}
	comments := make([]json.RawMessage, 0)
	for _, page := range pages {
		comments = append(comments, page...)
	}
	if err := json.NewEncoder(out).Encode(comments); err != nil {
		return fmt.Errorf("write review comments: %w", err)
	}
	return nil
}

func lastCommentPage(link string) (int, error) {
	for _, part := range strings.Split(link, ",") {
		target, attributes, ok := strings.Cut(part, ";")
		if !ok || !strings.Contains(attributes, `rel="last"`) {
			continue
		}
		parsed, err := url.Parse(strings.Trim(strings.TrimSpace(target), "<>"))
		if err != nil {
			return 0, fmt.Errorf("parse last-page link: %w", err)
		}
		page, err := strconv.Atoi(parsed.Query().Get("page"))
		if err != nil {
			return 0, fmt.Errorf("parse last-page number: %w", err)
		}
		if page < 1 {
			return 0, errors.New("last-page number must be positive")
		}
		return page, nil
	}
	return 1, nil
}
