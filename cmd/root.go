package cmd

import (
	"errors"

	"github.com/philprime/gh-kit/cmd/agent"
	"github.com/philprime/gh-kit/cmd/pr"
	"github.com/philprime/gh-kit/cmd/workflows"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	command := &cobra.Command{
		Use:         "kit",
		Annotations: map[string]string{cobra.CommandDisplayNameAnnotation: "gh kit"},
		Short:       "Everyday GitHub workflow utilities",
		Long: "Everyday GitHub workflow utilities.\n\n" +
			"Sentry telemetry is enabled by default for errors, panics, traces, logs, breadcrumbs, and metrics.\n" +
			"Set TELEMETRY_DISABLED=true to disable telemetry. Arguments and output are not sent.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("a command is required")
		},
	}
	command.CompletionOptions.DisableDefaultCmd = true
	command.AddCommand(workflows.NewWorkflowsCmd(), pr.NewPRCmd(), agent.NewAgentCmd())
	return command
}
