package workflows

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRerunInspectionFailureCancelsOtherRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	peerStarted := make(chan struct{})
	client := testClient(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42":
			return 200, `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
		case "/repos/owner/repo/actions/runs":
			return 200, `{"total_count":2,"workflow_runs":[{"id":1,"status":"completed","head_sha":"current","head_repository":{"id":7}},{"id":2,"status":"completed","head_sha":"current","head_repository":{"id":7}}]}`
		case "/repos/owner/repo/actions/runs/1/jobs":
			close(peerStarted)
			<-r.Context().Done()
			return 408, `{"message":"Request cancelled"}`
		case "/repos/owner/repo/actions/runs/2/jobs":
			select {
			case <-peerStarted:
			case <-ctx.Done():
				t.Error("other job inspection never started")
			}
			return 403, `{"message":"Forbidden"}`
		default:
			t.Errorf("unexpected request or mutation: %s %s", r.Method, r.URL)
			return 404, `{}`
		}
	})
	err := rerunWorkflows(ctx, client, "owner/repo", 42, false, true, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "workflow run 2") || !strings.Contains(err.Error(), "Forbidden") {
		t.Fatalf("expected original inspection error, got %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("inspection waited for the parent deadline instead of cancelling its peer")
	}
}

func TestRerunInspectsRunsConcurrentlyBeforeMutating(t *testing.T) {
	started := make(chan struct{}, 14)
	release := make(chan struct{})
	var active, peak, completed atomic.Int32
	var attempted []string
	var mutationMu sync.Mutex
	client := testClient(t, func(r *http.Request) (int, string) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/42"):
			return 200, `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
		case strings.HasSuffix(r.URL.Path, "/actions/runs"):
			var runs []string
			for id := 1; id <= 7; id++ {
				runs = append(runs, fmt.Sprintf(`{"id":%d,"status":"completed","head_sha":"current","head_repository":{"id":7}}`, id))
			}
			return 200, `{"total_count":7,"workflow_runs":[` + strings.Join(runs, ",") + `]}`
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/jobs"):
			current := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); current > old; old = peak.Load() {
				if peak.CompareAndSwap(old, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			var id, page int
			fmt.Sscanf(r.URL.Path, "/repos/owner/repo/actions/runs/%d/jobs", &id)
			fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
			if page != 1 && page != 2 {
				t.Errorf("unexpected page: %d", page)
			}
			completed.Add(1)
			return 200, fmt.Sprintf(`{"total_count":2,"jobs":[{"id":%d,"status":"completed","conclusion":"cancelled"}]}`, id*10+page)
		case r.Method == http.MethodPost:
			if completed.Load() != 14 {
				t.Error("rerun requested before every job page was inspected")
			}
			mutationMu.Lock()
			attempted = append(attempted, r.URL.Path)
			mutationMu.Unlock()
			return 201, ""
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			return 404, `{}`
		}
	})
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- rerunWorkflows(context.Background(), client, "owner/repo", 42, false, true, &out)
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for range 5 {
		select {
		case <-started:
		case <-timer.C:
			close(release)
			<-done
			t.Fatal("job inspection did not reach five concurrent requests")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 5 {
		t.Fatalf("concurrent requests exceeded limit: %d", peak.Load())
	}
	var want []string
	for id := 1; id <= 7; id++ {
		for page := 1; page <= 2; page++ {
			want = append(want, fmt.Sprintf("/repos/owner/repo/actions/jobs/%d/rerun", id*10+page))
		}
	}
	slices.Sort(attempted)
	if !reflect.DeepEqual(attempted, want) {
		t.Fatalf("rerun targets: %v, want %v", attempted, want)
	}
}
