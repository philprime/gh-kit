package main

import (
	"context"
	"io"

	"github.com/philprime/gh-kit/cmd"
	"github.com/philprime/gh-kit/internal/telemetry"
	"github.com/spf13/cobra"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	root := cmd.NewRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	instrumentCommands(root)
	return root.ExecuteContext(ctx)
}

func instrumentCommands(command *cobra.Command) {
	for _, child := range command.Commands() {
		instrumentCommands(child)
	}
	if command.RunE == nil || command.HasSubCommands() {
		return
	}
	runCommand := command.RunE
	command.RunE = func(command *cobra.Command, args []string) error {
		return telemetry.RunCommand(command.Context(), command.CommandPath(), func(ctx context.Context) error {
			command.SetContext(ctx)
			return runCommand(command, args)
		})
	}
}
