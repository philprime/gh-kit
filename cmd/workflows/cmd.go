package workflows

import (
	"errors"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/philprime/gh-kit/internal/repoflag"
	"github.com/spf13/cobra"
)

func NewWorkflowsCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "workflows",
		Short: "Manage GitHub Actions workflow runs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("a workflows subcommand is required")
		},
	}
	repoflag.Add(command)
	deps := dependencies{
		currentRepository: repository.Current,
		newRESTClient:     api.NewRESTClient,
	}
	command.AddCommand(newCancelAllCmd(deps), newLogsCmd(deps), newRerunCmd(deps))
	return command
}
