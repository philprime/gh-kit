package pr

import (
	"errors"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
)

func NewPRCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "pr",
		Short: "Read pull request information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("a pr subcommand is required")
		},
	}
	command.AddCommand(newCommentsCmd(dependencies{
		currentRepository: repository.Current,
		newRESTClient:     api.NewRESTClient,
	}))
	return command
}
