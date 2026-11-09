package repoflag

import (
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
)

func Add(command *cobra.Command) {
	command.Flags().StringP("repo", "R", "", "Select another repository using [HOST/]OWNER/REPO or a repository URL")
}

func Resolve(command *cobra.Command, current func() (repository.Repository, error)) (repository.Repository, error) {
	if command.Flags().Changed("repo") {
		value, err := command.Flags().GetString("repo")
		if err != nil {
			return repository.Repository{}, fmt.Errorf("read repository flag: %w", err)
		}
		repo, err := repository.Parse(value)
		if err != nil {
			return repository.Repository{}, fmt.Errorf("parse repository: %w", err)
		}
		return repo, nil
	}
	repo, err := current()
	if err != nil {
		return repository.Repository{}, fmt.Errorf("resolve current repository: %w", err)
	}
	return repo, nil
}
