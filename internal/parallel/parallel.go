package parallel

import (
	"context"
	"sync"
	"sync/atomic"
)

// Do runs indexed operations with at most five workers. Results retain input order.
// An operation's failure does not prevent other operations from being attempted.
func Do(ctx context.Context, count int, operation func(int) error) []error {
	results := make([]error, count)
	var next atomic.Int64
	var workers sync.WaitGroup
	for range min(5, count) {
		workers.Go(func() {
			for {
				index := int(next.Add(1)) - 1
				if index >= count {
					return
				}
				if err := ctx.Err(); err != nil {
					results[index] = err
					continue
				}
				results[index] = operation(index)
			}
		})
	}
	workers.Wait()
	return results
}
