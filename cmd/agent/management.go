package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

type installation struct {
	skill      skill
	store      store
	inspection inspection
}

// Preflight the entire selection before recovering or changing any skill.
// Once execution starts, return completed outcomes alongside a later failure.
func manage(skills []skill, root string, removing, dryRun, force bool, ops fileOperations) ([]string, error) {
	installations, err := inspectInstallations(skills, root, ops)
	if err != nil {
		return nil, err
	}
	return manageInstallations(installations, removing, dryRun, force)
}

func inspectInstallations(skills []skill, root string, ops fileOperations) ([]installation, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("skill installation root must not be empty")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve installation root: %w", err)
	}
	installations := make([]installation, 0, len(skills))
	for _, skill := range skills {
		store := store{root: root, name: skill.name, ops: ops}
		inspected, err := store.inspect()
		if err != nil {
			return nil, err
		}
		installations = append(installations, installation{skill: skill, store: store, inspection: inspected})
	}
	return installations, nil
}

func manageInstallations(installations []installation, removing, dryRun, force bool) ([]string, error) {
	for _, item := range installations {
		state := item.inspection.state
		if state.Content == nil {
			continue
		}
		if state.Receipt == nil {
			return nil, fmt.Errorf("refusing to change unmanaged skill %q", item.skill.name)
		}
		if removing {
			if !reflect.DeepEqual(state.Content, state.Receipt.Content) {
				return nil, fmt.Errorf("refusing to remove modified managed skill %q", item.skill.name)
			}
		} else if !reflect.DeepEqual(state.Content, []byte(item.skill.content)) && !force {
			return nil, fmt.Errorf("managed skill %q differs. Use --force to replace it", item.skill.name)
		}
	}
	var completed []string
	for _, item := range installations {
		result, err := item.execute(removing, dryRun)
		if err != nil {
			return completed, fmt.Errorf("%s: %w", item.skill.name, err)
		}
		completed = append(completed, result)
	}
	return completed, nil
}

func update(skills []skill, root string, dryRun bool, ops fileOperations) ([]string, error) {
	installations, err := inspectInstallations(skills, root, ops)
	if err != nil {
		return nil, err
	}
	var selected []installation
	for _, item := range installations {
		state := item.inspection.state
		if state.Content == nil || state.Receipt == nil {
			continue
		}
		if !reflect.DeepEqual(state.Content, state.Receipt.Content) {
			return nil, fmt.Errorf("managed skill %q has local edits. Resolve them before updating, or run 'gh kit agent skills install %s --force' to replace them", item.skill.name, item.skill.name)
		}
		selected = append(selected, item)
	}
	// Updates have already verified that every selected file matches its receipt.
	return manageInstallations(selected, false, dryRun, true)
}

func outcomeSummary(outcomes []string) string {
	if len(outcomes) == 0 {
		return "No skills to process."
	}
	var counts []string
	for _, label := range []string{"Installed", "Updated", "Unchanged", "Uninstalled", "Not installed", "Would install", "Would update", "Would uninstall"} {
		count := 0
		for _, outcome := range outcomes {
			if strings.HasPrefix(outcome, "Would recover pending operation: ") {
				_, outcome, _ = strings.Cut(outcome, "\n")
			}
			if strings.HasPrefix(outcome, label+":") {
				count++
			}
		}
		if count > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", count, strings.ToLower(label)))
		}
	}
	return "Summary: " + strings.Join(counts, ", ")
}

func (item installation) execute(removing, dryRun bool) (string, error) {
	if !dryRun {
		if err := item.store.recover(item.inspection); err != nil {
			return "", err
		}
	}
	state := item.inspection.state
	var result string
	if removing {
		switch {
		case state.Receipt == nil:
			result = "Not installed: " + item.skill.name
		case dryRun:
			result = "Would uninstall: " + item.skill.name
		default:
			if err := item.store.update(state, snapshot{}); err != nil {
				return "", err
			}
			result = "Uninstalled: " + item.skill.name
		}
	} else {
		content := []byte(item.skill.content)
		if reflect.DeepEqual(state.Content, content) && state.Receipt != nil && reflect.DeepEqual(state.Receipt.Content, content) {
			result = "Unchanged: " + item.skill.name
		} else if dryRun {
			verb := "install"
			if state.Content != nil {
				verb = "update"
			}
			result = fmt.Sprintf("Would %s: %s at %s", verb, item.skill.name, item.store.file())
		} else {
			next := snapshot{Content: content, Receipt: &receipt{Name: item.skill.name, Content: content}}
			if err := item.store.update(state, next); err != nil {
				return "", err
			}
			verb := "Installed"
			if state.Content != nil {
				verb = "Updated"
			}
			result = fmt.Sprintf("%s: %s at %s", verb, item.skill.name, item.store.file())
		}
	}
	if dryRun && item.inspection.pending != nil {
		result = "Would recover pending operation: " + item.skill.name + "\n" + result
	}
	return result, nil
}
