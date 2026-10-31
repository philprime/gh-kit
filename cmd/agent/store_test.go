package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(name, content string) skill { return skill{name: name, content: content} }

func TestBatchPreflight(t *testing.T) {
	for _, removing := range []bool{false, true} {
		t.Run(map[bool]string{false: "install", true: "uninstall"}[removing], func(t *testing.T) {
			root := t.TempDir()
			skills := []skill{fixture("first", "first"), fixture("second", "second")}
			if removing {
				if _, err := manage(skills, root, false, false, false, diskOperations()); err != nil {
					t.Fatal(err)
				}
			}
			conflict := filepath.Join(root, "skills/second/SKILL.md")
			put(t, conflict, "user edits")
			out, err := manage(skills, root, removing, false, false, diskOperations())
			if err == nil || len(out) != 0 {
				t.Fatalf("expected preflight failure: %v %v", out, err)
			}
			if removing {
				if read(t, filepath.Join(root, "skills/first/SKILL.md")) != "first" {
					t.Fatal("earlier skill changed")
				}
			} else {
				absent(t, filepath.Join(root, "skills/first"))
			}
			if read(t, conflict) != "user edits" {
				t.Fatal("conflict changed")
			}
		})
	}
}

func TestInterruptedOperations(t *testing.T) {
	for _, stage := range []string{"fresh receipt", "update receipt", "removal receipt", "journal cleanup"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			current := fixture("example", "new")
			removing := stage == "removal receipt"
			if removing || stage == "update receipt" {
				if _, err := manage([]skill{fixture("example", "old")}, root, false, false, false, diskOperations()); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := filepath.Join(root, "skills/example/notes.txt")
			put(t, sentinel, "keep")
			ops := diskOperations()
			failure := errors.New("injected failure")
			if removing || stage == "journal cleanup" {
				original := ops.remove
				ops.remove = func(path string) error {
					target := ".gh-kit-managed.json"
					if stage == "journal cleanup" {
						target = ".gh-kit-pending.json"
					}
					if filepath.Base(path) == target {
						return failure
					}
					return original(path)
				}
			} else {
				original := ops.write
				ops.write = func(path string, data []byte) error {
					if filepath.Base(path) == ".gh-kit-managed.json" {
						return failure
					}
					return original(path, data)
				}
			}
			if _, err := manage([]skill{current}, root, removing, false, true, ops); !errors.Is(err, failure) {
				t.Fatalf("expected injected failure: %v", err)
			}
			journal := filepath.Join(root, "skills/example/.gh-kit-pending.json")
			before := read(t, journal)
			results, err := manage([]skill{current}, root, removing, true, false, diskOperations())
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || !strings.Contains(results[0], "Would recover pending operation: example\n") {
				t.Fatalf("missing recovery preview: %v", results)
			}
			if read(t, journal) != before {
				t.Fatal("dry run changed journal")
			}
			results, err = manage([]skill{current}, root, removing, false, false, diskOperations())
			if err != nil {
				t.Fatal(err)
			}
			want := "Unchanged: example"
			if removing {
				want = "Not installed: example"
			}
			if len(results) != 1 || results[0] != want {
				t.Fatalf("got %v, want %s", results, want)
			}
			absent(t, journal)
			if read(t, sentinel) != "keep" {
				t.Fatal("unrelated file changed")
			}
			if removing {
				absent(t, filepath.Join(root, "skills/example/SKILL.md"))
			} else if read(t, filepath.Join(root, "skills/example/SKILL.md")) != "new" {
				t.Fatal("wrong recovered content")
			}
		})
	}
}

func TestRecoveryConflicts(t *testing.T) {
	for _, edited := range []string{"SKILL.md", ".gh-kit-managed.json"} {
		t.Run(edited, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "skills/example")
			put(t, filepath.Join(dir, "SKILL.md"), "new")
			put(t, filepath.Join(dir, ".gh-kit-pending.json"), `{"name":"example","previous":{},"next":{"content":"bmV3","receipt":{"name":"example","content":"bmV3"}}}`)
			edit := "external edits"
			if edited == ".gh-kit-managed.json" {
				edit = `{"name":"example","content":"ZWRpdHM="}`
			}
			put(t, filepath.Join(dir, edited), edit)
			if _, err := manage([]skill{fixture("example", "new")}, root, false, false, true, diskOperations()); err == nil {
				t.Fatal("expected recovery conflict")
			}
			if read(t, filepath.Join(dir, edited)) != edit {
				t.Fatal("external edits changed")
			}
			read(t, filepath.Join(dir, ".gh-kit-pending.json"))
		})
	}
}

func TestRecoveryPreflightAndPartialBatch(t *testing.T) {
	root := t.TempDir()
	skills := []skill{fixture("first", "first"), fixture("second", "second")}
	failure := errors.New("injected failure")
	ops := diskOperations()
	original := ops.write
	ops.write = func(path string, data []byte) error {
		if filepath.Base(filepath.Dir(path)) == "second" && filepath.Base(path) == ".gh-kit-managed.json" {
			return failure
		}
		return original(path, data)
	}
	completed, err := manage(skills, root, false, false, false, ops)
	if !errors.Is(err, failure) || len(completed) != 1 || !strings.HasPrefix(completed[0], "Installed: first at ") {
		t.Fatalf("unexpected partial result: %v %v", completed, err)
	}
	journal := filepath.Join(root, "skills/second/.gh-kit-pending.json")
	before := read(t, journal)
	put(t, filepath.Join(root, "skills/third/SKILL.md"), "unmanaged")
	_, err = manage([]skill{skills[1], fixture("third", "third")}, root, false, false, true, diskOperations())
	if err == nil {
		t.Fatal("expected preflight conflict")
	}
	if read(t, journal) != before {
		t.Fatal("preflight recovered earlier skill")
	}
	absent(t, filepath.Join(root, "skills/second/.gh-kit-managed.json"))
}

func TestPendingValidation(t *testing.T) {
	for _, journal := range []string{
		`{"name":"example","next":{}}`,
		`{"name":"example","previous":{},"next":{"content":"bmV3"}}`,
		`{"name":"example","previous":{"content":"b2xk"},"next":{}}`,
		`{"name":"example","previous":{},"next":{"content":"bmV3","receipt":{"name":"other","content":"bmV3"}}}`,
		`{"name":"example","previous":{},"next":{"content":"bmV3","receipt":{"name":"example","content":"b2xk"}}}`,
	} {
		t.Run(journal, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "skills/example/.gh-kit-pending.json")
			put(t, path, journal)
			if _, err := manage([]skill{fixture("example", "new")}, root, false, false, true, diskOperations()); err == nil {
				t.Fatal("expected invalid journal error")
			}
			if read(t, path) != journal {
				t.Fatal("invalid journal changed")
			}
			absent(t, filepath.Join(root, "skills/example/SKILL.md"))
		})
	}
}

func TestEmptySkillContent(t *testing.T) {
	root := t.TempDir()
	skills := []skill{fixture("empty", "")}
	if _, err := manage(skills, root, false, false, false, diskOperations()); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(root, "skills/empty/SKILL.md")) != "" {
		t.Fatal("expected empty file")
	}
	if _, err := manage(skills, root, true, false, false, diskOperations()); err != nil {
		t.Fatal(err)
	}
	absent(t, filepath.Join(root, "skills/empty"))
}

func TestRecoverUpdateBeforeUninstall(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills/example")
	put(t, filepath.Join(dir, "SKILL.md"), "new")
	put(t, filepath.Join(dir, ".gh-kit-managed.json"), `{"name":"example","content":"b2xk"}`)
	put(t, filepath.Join(dir, ".gh-kit-pending.json"), `{"name":"example","previous":{"content":"b2xk","receipt":{"name":"example","content":"b2xk"}},"next":{"content":"bmV3","receipt":{"name":"example","content":"bmV3"}}}`)
	results, err := manage([]skill{fixture("example", "new")}, root, true, false, false, diskOperations())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != "Uninstalled: example" {
		t.Fatalf("unexpected outcome: %v", results)
	}
	absent(t, dir)
}

func TestRepairReceiptsAndMissingContent(t *testing.T) {
	for _, missingContent := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale receipt", true: "missing content"}[missingContent], func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "skills/example")
			put(t, filepath.Join(dir, ".gh-kit-managed.json"), `{"name":"example","content":"b2xk"}`)
			if !missingContent {
				put(t, filepath.Join(dir, "SKILL.md"), "new")
			}
			results, err := manage([]skill{fixture("example", "new")}, root, false, false, false, diskOperations())
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || !strings.HasPrefix(results[0], "Installed: example at ") {
				t.Fatalf("unexpected repair result: %v", results)
			}
			if read(t, filepath.Join(dir, "SKILL.md")) != "new" {
				t.Fatal("wrong content")
			}
			if _, err := manage([]skill{fixture("example", "new")}, root, true, false, false, diskOperations()); err != nil {
				t.Fatal(err)
			}
			absent(t, dir)
		})
	}
}

func TestAtomicWriteFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing/file")
	if err := atomicWrite(path, []byte("content")); err == nil {
		t.Fatal("expected write error")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected artifacts: %v %v", entries, err)
	}
}
