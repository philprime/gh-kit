package parallel

import (
	"context"
	"errors"
	"testing"
)

func TestDoPreservesErrorsAndAttemptsAllOperations(t *testing.T) {
	failure := errors.New("operation failed")
	called := make([]bool, 9)
	results := Do(context.Background(), len(called), func(index int) error {
		called[index] = true
		if index == 3 {
			return failure
		}
		return nil
	})
	for index, err := range results {
		if !called[index] {
			t.Fatalf("operation %d not attempted", index)
		}
		if (index == 3) != errors.Is(err, failure) {
			t.Fatalf("unexpected result at %d: %v", index, err)
		}
	}
}

func TestDoCancelledContextDoesNotStartOperations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := Do(ctx, 8, func(int) error { t.Error("operation started after cancellation"); return nil })
	for _, err := range results {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	}
}

func TestDoEmpty(t *testing.T) {
	if results := Do(context.Background(), 0, func(int) error { t.Error("unexpected operation"); return nil }); len(results) != 0 {
		t.Fatalf("unexpected results: %v", results)
	}
}
