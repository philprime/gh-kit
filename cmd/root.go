package cmd

import (
	"errors"

	"github.com/philprime/gh-kit/cmd/pr"
	"github.com/philprime/gh-kit/cmd/workflows"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "kit",
		Annotations:   map[string]string{cobra.CommandDisplayNameAnnotation: "gh kit"},
		Short:         "Everyday GitHub workflow utilities",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("a command is required")
		},
	}
	command.CompletionOptions.DisableDefaultCmd = true
	command.AddCommand(workflows.NewWorkflowsCmd(), pr.NewPRCmd())
	return command
}
