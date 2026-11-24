package workflows

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkflowMutationsAreBoundedAndConcurrent(t *testing.T) {
	for _, action := range []string{"cancel", "rerun"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			started := make(chan struct{}, 7)
			release := make(chan struct{})
			var active, peak, attempts atomic.Int32
			client := testClient(t, func(r *http.Request) (int, string) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/pulls/42"):
					return 200, `{"head":{"ref":"topic","sha":"current","repo":{"id":7}}}`
				case strings.HasSuffix(r.URL.Path, "/actions/runs"):
					var runs []string
					status := "completed"
					if action == "cancel" {
						status = "queued"
					}
					for id := 1; id <= 7; id++ {
						runs = append(runs, fmt.Sprintf(`{"id":%d,"status":%q,"head_sha":"current","head_repository":{"id":7}}`, id, status))
					}
					return 200, `{"total_count":7,"workflow_runs":[` + strings.Join(runs, ",") + `]}`
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/jobs"):
					var id int
					fmt.Sscanf(r.URL.Path, "/repos/owner/repo/actions/runs/%d/jobs", &id)
					return 200, fmt.Sprintf(`{"total_count":1,"jobs":[{"id":%d,"status":"completed","conclusion":"failure"}]}`, id)
				case r.Method == http.MethodPost:
					attempts.Add(1)
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
						return 408, `{"message":"cancelled"}`
					}
					if strings.HasSuffix(r.URL.Path, "/3/"+action) {
						return 403, `{"message":"Forbidden"}`
					}
					return 202, ""
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					return 404, `{}`
				}
			})
			var out bytes.Buffer
			done := make(chan error, 1)
			go func() {
				if action == "cancel" {
					done <- cancelAllWorkflows(ctx, client, "owner/repo", 42, &out)
				} else {
					done <- rerunWorkflows(ctx, client, "owner/repo", 42, true, false, &out)
				}
			}()
			for range 5 {
				select {
				case <-started:
				case <-ctx.Done():
					close(release)
					<-done
					t.Fatal("mutations did not reach five concurrent requests")
				}
			}
			close(release)
			err := <-done
			if err == nil || !strings.Contains(err.Error(), "3") {
				t.Fatalf("missing partial failure: %v", err)
			}
			if attempts.Load() != 7 || peak.Load() > 5 {
				t.Fatalf("attempts=%d peak=%d", attempts.Load(), peak.Load())
			}
			summary := "Requested cancellation of 6 workflow run(s)"
			if action == "rerun" {
				summary = "Requested reruns for 6 job(s)"
			}
			if !strings.Contains(out.String(), summary) {
				t.Fatalf("incorrect success summary: %s", &out)
			}
			previous := -1
			for _, id := range []int{1, 2, 4, 5, 6, 7} {
				message := fmt.Sprintf("Cancellation requested for workflow run %d.", id)
				if action == "rerun" {
					message = fmt.Sprintf("Rerun requested for job %d.", id)
				}
				position := strings.Index(out.String(), message)
				if position <= previous {
					t.Fatalf("confirmations out of discovery order: %s", &out)
				}
				previous = position
			}
		})
	}
}
