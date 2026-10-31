package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

type receipt struct {
	Name    string `json:"name"`
	Content []byte `json:"content"`
}

type snapshot struct {
	Content []byte   `json:"content"`
	Receipt *receipt `json:"receipt"`
}

type pending struct {
	Name     string    `json:"name"`
	Previous *snapshot `json:"previous"`
	Next     *snapshot `json:"next"`
}

type inspection struct {
	state   snapshot
	pending *pending
}

type fileOperations struct {
	write  func(string, []byte) error
	remove func(string) error
}

func diskOperations() fileOperations { return fileOperations{write: atomicWrite, remove: os.Remove} }

// Each file is replaced atomically. The journal coordinates the content and receipt
// so a subsequent invocation can finish an interrupted operation.
func atomicWrite(path string, data []byte) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".gh-kit-write-*")
	if err != nil {
		return fmt.Errorf("create temporary skill file: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temporary skill file: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close temporary skill file: %w", err)
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace skill file: %w", err)
	}
	return nil
}

// A store owns only SKILL.md, its receipt, and its recovery journal, never the
// entire skill directory.
type store struct {
	root, name string
	ops        fileOperations
}

func (s store) directory() string   { return filepath.Join(s.root, "skills", s.name) }
func (s store) file() string        { return filepath.Join(s.directory(), "SKILL.md") }
func (s store) receiptFile() string { return filepath.Join(s.directory(), ".gh-kit-managed.json") }
func (s store) pendingFile() string { return filepath.Join(s.directory(), ".gh-kit-pending.json") }

func (s store) validateDirectory() error {
	for path := s.directory(); ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect skill directory: %w", err)
		}
		if err == nil && !info.IsDir() {
			return fmt.Errorf("refusing symlink or non-directory at %s", path)
		}
		// Ancestors above the selected root may be OS aliases such as /var on macOS.
		if path == s.root {
			return nil
		}
	}
}

func regularData(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect skill file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing symlink or non-regular file at %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill file: %w", err)
	}
	return data, nil
}

func (s store) snapshot() (snapshot, error) {
	content, err := regularData(s.file())
	if err != nil {
		return snapshot{}, err
	}
	data, err := regularData(s.receiptFile())
	if err != nil {
		return snapshot{}, err
	}
	state := snapshot{Content: content}
	if data != nil {
		var r receipt
		if err := json.Unmarshal(data, &r); err != nil || r.Name != s.name || r.Content == nil {
			return snapshot{}, fmt.Errorf("invalid skill ownership receipt at %s", s.receiptFile())
		}
		state.Receipt = &r
	}
	return state, nil
}

func validReceipt(r *receipt, name string) bool {
	return r == nil || r.Name == name && r.Content != nil
}

func (s store) readPending() (*pending, error) {
	data, err := regularData(s.pendingFile())
	if err != nil || data == nil {
		return nil, err
	}
	var p pending
	if err := json.Unmarshal(data, &p); err != nil || p.Name != s.name || p.Previous == nil || p.Next == nil {
		return nil, fmt.Errorf("invalid pending skill operation at %s", s.pendingFile())
	}
	validPrevious := validReceipt(p.Previous.Receipt, s.name) && (p.Previous.Content == nil || p.Previous.Receipt != nil)
	validNext := reflect.DeepEqual(*p.Next, snapshot{}) || p.Next.Receipt != nil && validReceipt(p.Next.Receipt, s.name) && reflect.DeepEqual(p.Next.Content, p.Next.Receipt.Content)
	if !validPrevious || !validNext {
		return nil, fmt.Errorf("invalid pending skill operation at %s", s.pendingFile())
	}
	return &p, nil
}

func (s store) conflict() error { return fmt.Errorf("skill recovery conflict at %s", s.directory()) }

func (s store) validate(current snapshot, p *pending) error {
	if (!reflect.DeepEqual(current.Content, p.Previous.Content) && !reflect.DeepEqual(current.Content, p.Next.Content)) ||
		(!reflect.DeepEqual(current.Receipt, p.Previous.Receipt) && !reflect.DeepEqual(current.Receipt, p.Next.Receipt)) {
		return s.conflict()
	}
	return nil
}

func (s store) inspect() (inspection, error) {
	if err := s.validateDirectory(); err != nil {
		return inspection{}, err
	}
	state, err := s.snapshot()
	if err != nil {
		return inspection{}, err
	}
	p, err := s.readPending()
	if err != nil {
		return inspection{}, err
	}
	if p != nil {
		if err := s.validate(state, p); err != nil {
			return inspection{}, err
		}
		state = *p.Next
	}
	return inspection{state: state, pending: p}, nil
}

func (s store) recover(expected inspection) error {
	current, err := s.inspect()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, expected) {
		return s.conflict()
	}
	if current.pending != nil {
		return s.apply(current.pending)
	}
	return nil
}

func (s store) update(previous, next snapshot) error {
	current, err := s.inspect()
	if err != nil {
		return err
	}
	if current.pending != nil || !reflect.DeepEqual(current.state, previous) {
		return s.conflict()
	}
	if err := os.MkdirAll(s.directory(), 0755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	p := &pending{Name: s.name, Previous: &previous, Next: &next}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode pending skill operation: %w", err)
	}
	if err := s.ops.write(s.pendingFile(), data); err != nil {
		return fmt.Errorf("write pending skill operation: %w", err)
	}
	return s.apply(p)
}

func (s store) apply(p *pending) error {
	if err := s.validateDirectory(); err != nil {
		return err
	}
	onDisk, err := s.readPending()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(onDisk, p) {
		return s.conflict()
	}
	current, err := s.snapshot()
	if err != nil {
		return err
	}
	if err := s.validate(current, p); err != nil {
		return err
	}
	if !reflect.DeepEqual(current.Content, p.Next.Content) {
		if p.Next.Content == nil {
			err = s.ops.remove(s.file())
		} else {
			err = s.ops.write(s.file(), p.Next.Content)
		}
		if err != nil {
			return fmt.Errorf("update skill content: %w", err)
		}
	}
	if !reflect.DeepEqual(current.Receipt, p.Next.Receipt) {
		if p.Next.Receipt == nil {
			err = s.ops.remove(s.receiptFile())
		} else {
			var data []byte
			data, err = json.Marshal(p.Next.Receipt)
			if err == nil {
				err = s.ops.write(s.receiptFile(), data)
			}
		}
		if err != nil {
			return fmt.Errorf("update skill receipt: %w", err)
		}
	}
	after, err := s.snapshot()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(after, *p.Next) {
		return s.conflict()
	}
	if err := s.ops.remove(s.pendingFile()); err != nil {
		return fmt.Errorf("remove pending skill operation: %w", err)
	}
	if reflect.DeepEqual(*p.Next, snapshot{}) {
		entries, err := os.ReadDir(s.directory())
		if err != nil {
			return fmt.Errorf("inspect emptied skill directory: %w", err)
		}
		if len(entries) == 0 {
			if err := s.ops.remove(s.directory()); err != nil {
				return fmt.Errorf("remove empty skill directory: %w", err)
			}
		}
	}
	return nil
}
