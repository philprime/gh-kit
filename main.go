package main

import (
	"context"
	"fmt"
	"os"

	"github.com/philprime/gh-kit/internal/telemetry"
)

func main() {
	err := func() error {
		ctx, shutdown := telemetry.Init(context.Background())
		defer shutdown()
		return run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
