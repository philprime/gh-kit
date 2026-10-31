package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func NewAgentCmd() *cobra.Command {
	command := &cobra.Command{
		Use: "agent", Short: "Manage agent integrations", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return errors.New("an agent subcommand is required") },
	}
	skills := &cobra.Command{
		Use: "skills", Short: "Access portable Agent Skills bundled with gh-kit", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return errors.New("a skills subcommand is required") },
	}
	skills.AddCommand(&cobra.Command{
		Use: "list", Short: "List bundled Agent Skills", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, s := range bundled {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", s.name, s.description); err != nil {
					return fmt.Errorf("write skill list: %w", err)
				}
			}
			return nil
		},
	}, &cobra.Command{
		Use: "get <name>", Short: "Print a bundled Agent Skill", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, ok := findSkill(args[0])
			if !ok {
				return unknownSkill(args[0])
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), s.content); err != nil {
				return fmt.Errorf("write skill: %w", err)
			}
			return nil
		},
	}, newManagementCmd(false), newManagementCmd(true))
	command.AddCommand(skills)
	return command
}

func unknownSkill(name string) error {
	return fmt.Errorf("unknown bundled Agent Skill %q. Run 'gh kit agent skills list'", name)
}

func newManagementCmd(removing bool) *cobra.Command {
	var all, project, dryRun, force, yes bool
	var dir string
	verb := "install"
	short := "Install bundled Agent Skills into a .agents directory"
	details := "Existing unmanaged files and symlinks are never overwritten, even with --force."
	if removing {
		verb = "uninstall"
		short = "Remove skills managed by gh-kit from a .agents directory"
		details = "Removes only unchanged managed files and preserves unrelated files.\nBulk removal requires --yes unless --dry-run is set. No interactive prompt is used."
	}
	command := &cobra.Command{
		Use: verb + " [names...]", Short: short,
		Long:    short + ".\n\nDefaults to ~/.agents/skills. Use --project for the Git root's .agents directory\n(current directory outside Git), or --dir for another .agents root.\n" + details,
		Example: "  gh kit agent skills " + verb + " gh-kit\n  gh kit agent skills " + verb + " --all --dry-run",
		RunE: func(cmd *cobra.Command, names []string) error {
			if project && cmd.Flags().Changed("dir") {
				return errors.New("use either --project or --dir, not both")
			}
			if all == (len(names) != 0) {
				return errors.New("specify one or more skill names, or --all, but not both")
			}
			var selected []skill
			if all {
				selected = bundled
			} else {
				seen := make(map[string]bool)
				for _, name := range names {
					s, ok := findSkill(name)
					if !ok {
						return unknownSkill(name)
					}
					if !seen[name] {
						selected = append(selected, s)
						seen[name] = true
					}
				}
			}
			if removing && all && !yes && !dryRun {
				return errors.New("uninstalling all skills requires --yes. Use --dry-run to preview first")
			}
			root, err := installationRoot(dir, cmd.Flags().Changed("dir"), project)
			if err != nil {
				return err
			}
			completed, operationErr := manage(selected, root, removing, dryRun, force, diskOperations())
			for _, result := range completed {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), result); err != nil {
					return errors.Join(operationErr, fmt.Errorf("write skill outcome: %w", err))
				}
			}
			return operationErr
		},
	}
	command.Flags().BoolVar(&all, "all", false, "Select all bundled skills instead of named skills")
	command.Flags().StringVar(&dir, "dir", "", "Root of the .agents installation (skills are stored under DIR/skills)")
	command.Flags().BoolVar(&project, "project", false, "Use the Git root's .agents directory (current directory outside Git)")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing or removing files")
	if removing {
		command.Flags().BoolVarP(&yes, "yes", "y", false, "Approve uninstalling all bundled skills")
	} else {
		command.Flags().BoolVar(&force, "force", false, "Overwrite differing SKILL.md files only when managed by gh-kit")
	}
	return command
}

func installationRoot(dir string, explicit, project bool) (string, error) {
	if project {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve current directory: %w", err)
		}
		for ancestor := cwd; ; ancestor = filepath.Dir(ancestor) {
			if _, err := os.Stat(filepath.Join(ancestor, ".git")); err == nil {
				return filepath.Join(ancestor, ".agents"), nil
			}
			if filepath.Dir(ancestor) == ancestor {
				return filepath.Join(cwd, ".agents"), nil
			}
		}
	}
	if !explicit || dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if !explicit {
			dir = filepath.Join(home, ".agents")
		} else if dir == "~" {
			dir = home
		} else {
			dir = filepath.Join(home, dir[2:])
		}
	}
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("skill installation root must not be empty")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve installation root: %w", err)
	}
	return root, nil
}
