package main

import (
	"context"
	"io"

	"github.com/philprime/gh-kit/cmd"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	root := cmd.NewRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return root.ExecuteContext(ctx)
}
