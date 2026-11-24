package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	command := NewAgentCmd()
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs(append([]string{"skills"}, args...))
	err := command.Execute()
	return out.String(), err
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected absent %s, got %v", path, err)
	}
}
func runOK(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execute(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSkillUpdate(t *testing.T) {
	for _, selection := range [][]string{nil, {"--all"}, {"gh-kit", "gh-kit"}} {
		t.Run(strings.Join(selection, " "), func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "skills/gh-kit/SKILL.md")
			if _, err := manage([]skill{fixture("gh-kit", "previous version")}, root, false, false, false, diskOperations()); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"update"}, selection...)
			args = append(args, "--dir", root)
			out := runOK(t, append(args, "--dry-run")...)
			if out != "Would update: gh-kit at "+file+"\nSummary: 1 would update\n" || read(t, file) != "previous version" {
				t.Fatalf("unsafe preview: %q", out)
			}
			out = runOK(t, args...)
			if out != "Updated: gh-kit at "+file+"\nSummary: 1 updated\n" || read(t, file) != runOK(t, "get", "gh-kit") {
				t.Fatalf("unexpected update: %q", out)
			}
			if out := runOK(t, args...); out != "Unchanged: gh-kit\nSummary: 1 unchanged\n" {
				t.Fatalf("unexpected repeated update: %q", out)
			}
		})
	}
}

func TestSkillUpdateSkipsUninstalledAndProtectsEdits(t *testing.T) {
	for _, state := range []string{"absent", "unmanaged", "missing", "modified"} {
		t.Run(state, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			file := filepath.Join(root, "skills/gh-kit/SKILL.md")
			if state == "missing" || state == "modified" {
				if _, err := manage([]skill{fixture("gh-kit", "old")}, root, false, false, false, diskOperations()); err != nil {
					t.Fatal(err)
				}
			}
			if state == "missing" {
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			}
			if state == "modified" || state == "unmanaged" {
				put(t, file, "local edits")
			}
			for _, dry := range []bool{false, true} {
				args := []string{"update", "--dir", root}
				if dry {
					args = append(args, "--dry-run")
				}
				out, err := execute(t, args...)
				if state == "modified" {
					if err == nil || !strings.Contains(err.Error(), "install gh-kit --force") {
						t.Fatalf("expected local-edit conflict: %v", err)
					}
				} else if err != nil || out != "No skills to process.\n" {
					t.Fatalf("unexpected skipped result: %q %v", out, err)
				}
			}
			if state == "absent" {
				absent(t, root)
			}
			if state == "missing" {
				absent(t, file)
				read(t, filepath.Join(root, "skills/gh-kit/.gh-kit-managed.json"))
			}
			if state == "modified" || state == "unmanaged" {
				if read(t, file) != "local edits" {
					t.Fatal("local edits changed")
				}
			}
		})
	}
}

func TestSkillListStatuses(t *testing.T) {
	for _, status := range []string{"not installed", "installed", "update available", "modified", "unmanaged", "missing"} {
		t.Run(status, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			file := filepath.Join(root, "skills/gh-kit/SKILL.md")
			if status == "installed" || status == "modified" || status == "missing" {
				runOK(t, "install", "gh-kit", "--dir", root)
			}
			if status == "update available" {
				if _, err := manage([]skill{fixture("gh-kit", "old")}, root, false, false, false, diskOperations()); err != nil {
					t.Fatal(err)
				}
			}
			if status == "modified" || status == "unmanaged" {
				put(t, file, "local edits")
			}
			if status == "missing" {
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			}
			out := runOK(t, "list", "--dir", root)
			if !strings.HasPrefix(out, "gh-kit\t"+status+"\t"+file+"\t") {
				t.Fatalf("wrong status: %q", out)
			}
			if status == "not installed" {
				absent(t, root)
			}
			if status == "missing" {
				absent(t, file)
			}
			if status == "modified" || status == "unmanaged" {
				if read(t, file) != "local edits" {
					t.Fatal("listing changed content")
				}
			}
		})
	}
}

func TestSkillLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".agents")
	file := filepath.Join(root, "skills/gh-kit/SKILL.md")
	if out := runOK(t, "install", "--all", "--dry-run", "--dir", root); out != "Would install: gh-kit at "+file+"\nSummary: 1 would install\n" {
		t.Fatalf("unexpected preview: %q", out)
	}
	absent(t, root)
	if out := runOK(t, "install", "gh-kit", "gh-kit", "--dir", root); out != "Installed: gh-kit at "+file+"\nSummary: 1 installed\n" {
		t.Fatalf("unexpected install: %q", out)
	}
	content := runOK(t, "get", "gh-kit")
	if read(t, file) != content {
		t.Fatal("installed content differs from get")
	}
	if out := runOK(t, "install", "--all", "--dir", root); out != "Unchanged: gh-kit\nSummary: 1 unchanged\n" {
		t.Fatalf("unexpected reinstall: %q", out)
	}
	if out := runOK(t, "uninstall", "--all", "--dry-run", "--dir", root); out != "Would uninstall: gh-kit\nSummary: 1 would uninstall\n" {
		t.Fatalf("unexpected preview: %q", out)
	}
	if read(t, file) != content {
		t.Fatal("dry run changed skill")
	}
	put(t, filepath.Join(root, "skills/gh-kit/notes.txt"), "keep")
	put(t, filepath.Join(root, "skills/custom/SKILL.md"), "custom")
	if out := runOK(t, "uninstall", "--all", "-y", "--dir", root); out != "Uninstalled: gh-kit\nSummary: 1 uninstalled\n" {
		t.Fatalf("unexpected removal: %q", out)
	}
	absent(t, file)
	absent(t, filepath.Join(root, "skills/gh-kit/.gh-kit-managed.json"))
	if read(t, filepath.Join(root, "skills/gh-kit/notes.txt")) != "keep" || read(t, filepath.Join(root, "skills/custom/SKILL.md")) != "custom" {
		t.Fatal("unrelated files changed")
	}
	if out := runOK(t, "uninstall", "gh-kit", "--dir", root); out != "Not installed: gh-kit\nSummary: 1 not installed\n" {
		t.Fatalf("unexpected absent removal: %q", out)
	}
}

func TestSkillOwnership(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmanaged", true: "modified"}[managed], func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "skills/gh-kit/SKILL.md")
			if managed {
				runOK(t, "install", "gh-kit", "--dir", root)
			}
			put(t, file, "local edits")
			for _, args := range [][]string{{"install", "gh-kit"}, {"uninstall", "gh-kit"}, {"install", "gh-kit", "--dry-run"}} {
				if _, err := execute(t, append(args, "--dir", root)...); err == nil {
					t.Fatal("expected ownership conflict")
				}
				if read(t, file) != "local edits" {
					t.Fatal("local edits changed")
				}
			}
			_, err := execute(t, "install", "gh-kit", "--force", "--dir", root)
			if managed {
				if err != nil {
					t.Fatal(err)
				}
				if read(t, file) != runOK(t, "get", "gh-kit") {
					t.Fatal("force did not restore content")
				}
			} else {
				if err == nil {
					t.Fatal("force overwrote unmanaged file")
				}
				if read(t, file) != "local edits" {
					t.Fatal("unmanaged content changed")
				}
			}
		})
	}
}

func TestSkillRoots(t *testing.T) {
	t.Chdir(t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	runOK(t, "install", "gh-kit")
	read(t, filepath.Join(home, ".agents/skills/gh-kit/SKILL.md"))
	if out := runOK(t, "update"); !strings.HasPrefix(out, "Unchanged: gh-kit\n") {
		t.Fatalf("wrong default update root: %q", out)
	}
	if out := runOK(t, "list"); !strings.Contains(out, filepath.Join(home, ".agents/skills/gh-kit/SKILL.md")) {
		t.Fatalf("wrong default list root: %q", out)
	}
	runOK(t, "uninstall", "gh-kit")
	absent(t, filepath.Join(home, ".agents/skills/gh-kit"))
	for _, marker := range []string{"none", "directory", "file"} {
		t.Run(marker, func(t *testing.T) {
			root := t.TempDir()
			nested := filepath.Join(root, "src/nested")
			if err := os.MkdirAll(nested, 0755); err != nil {
				t.Fatal(err)
			}
			switch marker {
			case "directory":
				if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
					t.Fatal(err)
				}
			case "file":
				put(t, filepath.Join(root, ".git"), "gitdir: elsewhere")
			}
			t.Chdir(nested)
			target := root
			if marker == "none" {
				target = nested
			}
			runOK(t, "install", "--all", "--project")
			read(t, filepath.Join(target, ".agents/skills/gh-kit/SKILL.md"))
			if out := runOK(t, "update", "--project"); !strings.HasPrefix(out, "Unchanged: gh-kit\n") {
				t.Fatalf("wrong project update root: %q", out)
			}
			if out := runOK(t, "list", "--project"); !strings.Contains(out, filepath.Join(target, ".agents/skills/gh-kit/SKILL.md")) {
				t.Fatalf("wrong project list root: %q", out)
			}
			runOK(t, "uninstall", "--all", "--yes", "--project")
			absent(t, filepath.Join(target, ".agents/skills/gh-kit"))
		})
	}
	root := filepath.Join(home, "custom")
	runOK(t, "install", "gh-kit", "--dir", "~/custom")
	read(t, filepath.Join(root, "skills/gh-kit/SKILL.md"))
}

func TestSkillInvalidMetadata(t *testing.T) {
	for _, filename := range []string{".gh-kit-managed.json", ".gh-kit-pending.json"} {
		for _, content := range []string{"not json", "{}", `{"name":"other","content":""}`, `{"name":"gh-kit"}`} {
			t.Run(filename+content, func(t *testing.T) {
				root := t.TempDir()
				runOK(t, "install", "gh-kit", "--dir", root)
				path := filepath.Join(root, "skills/gh-kit", filename)
				put(t, path, content)
				original := read(t, filepath.Join(root, "skills/gh-kit/SKILL.md"))
				for _, args := range [][]string{{"install", "gh-kit", "--force"}, {"uninstall", "gh-kit"}, {"update"}, {"list"}} {
					if _, err := execute(t, append(args, "--dir", root)...); err == nil {
						t.Fatal("expected invalid metadata error")
					}
				}
				if read(t, path) != content || read(t, filepath.Join(root, "skills/gh-kit/SKILL.md")) != original {
					t.Fatal("invalid metadata changed")
				}
			})
		}
	}
}

func TestSkillSymlinksAndNonRegularFiles(t *testing.T) {
	for _, relative := range []string{"", "skills", "skills/gh-kit", "skills/gh-kit/SKILL.md", "skills/gh-kit/.gh-kit-managed.json", "skills/gh-kit/.gh-kit-pending.json"} {
		for _, kind := range []string{"symlink", "wrong type"} {
			t.Run(relative+kind, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "root")
				outside := t.TempDir()
				put(t, filepath.Join(outside, "sentinel"), "keep")
				path := filepath.Join(root, relative)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					if err := os.Symlink(outside, path); err != nil {
						t.Skipf("symlink unavailable: %v", err)
					}
				} else if strings.Contains(relative, ".") {
					if err := os.Mkdir(path, 0755); err != nil {
						t.Fatal(err)
					}
				} else {
					put(t, path, "not directory")
				}
				for _, args := range [][]string{{"install", "gh-kit", "--force"}, {"uninstall", "gh-kit"}, {"update"}, {"list"}} {
					if _, err := execute(t, append(args, "--dir", root)...); err == nil {
						t.Fatal("expected unsafe path error")
					}
				}
				if read(t, filepath.Join(outside, "sentinel")) != "keep" {
					t.Fatal("symlink target changed")
				}
			})
		}
	}
}
