package pr

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

func TestCommentsPageFailureWritesNoPartialJSON(t *testing.T) {
	client, err := api.NewRESTClient(api.ClientOptions{
		Host: "github.com", AuthToken: "test-token", LogIgnoreEnv: true,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			header := http.Header{"Content-Type": {"application/json"}}
			header.Set("Link", `<https://api.github.com/repos/owner/repo/pulls/42/comments?page=3>; rel="last"`)
			status, body := 200, `[{"id":1}]`
			if r.URL.Query().Get("page") == "2" {
				status, body = 403, `{"message":"Forbidden"}`
			}
			return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = readComments(context.Background(), client, "owner/repo", 42, &out)
	if err == nil || !strings.Contains(err.Error(), "page 2") || out.Len() != 0 {
		t.Fatalf("error=%v output=%q", err, &out)
	}
}

func TestLastCommentPage(t *testing.T) {
	for _, tc := range []struct {
		link      string
		want      int
		wantError bool
	}{
		{"", 1, false},
		{`<https://api.github.com/comments?page=2>; rel="next", <https://api.github.com/comments?page=8>; rel="last"`, 8, false},
		{`<https://api.github.com/comments?page=0>; rel="last"`, 0, true},
		{`<https://api.github.com/comments?page=invalid>; rel="last"`, 0, true},
		{`<https://%invalid/comments?page=2>; rel="last"`, 0, true},
	} {
		t.Run(tc.link, func(t *testing.T) {
			got, err := lastCommentPage(tc.link)
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("page=%d error=%v", got, err)
			}
		})
	}
}

func TestCommentsFetchPagesConcurrentlyInOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := make(chan struct{}, 7)
	release := make(chan struct{})
	lastFinished := make(chan struct{})
	var active, peak, requests atomic.Int32
	client, err := api.NewRESTClient(api.ClientOptions{
		Host: "github.com", AuthToken: "test-token", LogIgnoreEnv: true,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests.Add(1)
			var page int
			fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
			if page > 1 {
				current := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); current > old; old = peak.Load() {
					if peak.CompareAndSwap(old, current) {
						break
					}
				}
				started <- struct{}{}
				select {
				case <-release:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			if page == 8 {
				close(lastFinished)
			}
			if page == 2 {
				select {
				case <-lastFinished:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			header := http.Header{"Content-Type": {"application/json"}}
			header.Set("Link", `<https://api.github.com/repos/owner/repo/pulls/42/comments?per_page=100&page=8>; rel="last"`)
			comments := []string{fmt.Sprintf(`{"id":%d}`, page)}
			if page == 1 {
				for range 99 {
					comments = append(comments, `{"id":1}`)
				}
			}
			return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader("[" + strings.Join(comments, ",") + "]")), Request: r}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- readComments(ctx, client, "owner/repo", 42, &out) }()
	for range 5 {
		select {
		case <-started:
		case <-ctx.Done():
			close(release)
			<-done
			t.Fatal("comment pages did not reach five concurrent requests")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 5 || requests.Load() != 8 {
		t.Fatalf("peak=%d requests=%d", peak.Load(), requests.Load())
	}
	want := "[" + strings.Repeat(`{"id":1},`, 100) + `{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8}]` + "\n"
	if out.String() != want {
		t.Fatalf("comments out of order: %s", &out)
	}
}
