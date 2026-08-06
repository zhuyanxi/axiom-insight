package commit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
)

// FileWriter abstracts the filesystem operations of a commit so the
// pipeline can run against a fake writer and faults can be injected at
// every point.
type FileWriter interface {
	// MkdirAll creates the directory and parents.
	MkdirAll(path string, perm os.FileMode) error
	// Lstat returns the file info without following symlinks.
	Lstat(path string) (os.FileInfo, error)
	// CreateExclusive creates a file with O_CREATE|O_EXCL and the given
	// permissions; it fails if the path already exists.
	CreateExclusive(path string, perm os.FileMode) (WriteCloserSyncer, error)
	// Rename replaces oldPath with newPath atomically on the same volume.
	Rename(oldPath, newPath string) error
	// Remove deletes a file.
	Remove(path string) error
}

// WriteCloserSyncer is a file handle used by the commit journal.
type WriteCloserSyncer interface {
	Write(contents []byte) (int, error)
	Sync() error
	Close() error
}

// OSFileWriter is the real filesystem implementation.
type OSFileWriter struct{}

// MkdirAll implements FileWriter.
func (OSFileWriter) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }

// Lstat implements FileWriter.
func (OSFileWriter) Lstat(path string) (os.FileInfo, error) { return os.Lstat(path) }

// CreateExclusive implements FileWriter.
func (OSFileWriter) CreateExclusive(path string, perm os.FileMode) (WriteCloserSyncer, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return nil, err
	}
	return file, nil
}

// Rename implements FileWriter.
func (OSFileWriter) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

// Remove implements FileWriter.
func (OSFileWriter) Remove(path string) error { return os.Remove(path) }

// Target is one file to commit into the output directory.
type Target struct {
	// Name is the file name inside the output directory, e.g.
	// "metrics.yaml". Only managed names are ever replaced.
	Name string
	// Contents are the fully validated bytes to commit.
	Contents []byte
}

// Commit journals a multi-file atomic-per-file replacement into one
// output directory. Create one Commit per generation run; Commit, then
// Rollback on failure, then always Cleanup.
type Commit struct {
	writer  FileWriter
	dir     string
	targets []Target
	lock    string

	journal []journalEntry
}

type journalEntry struct {
	name          string
	existedBefore bool
	backup        string
	temp          string
	committed     bool
}

// New validates the output directory path and target names without
// touching the filesystem. The directory must be relative or absolute,
// NUL-free, and cleaned; names must be plain file names.
func New(writer FileWriter, dir string, targets []Target) (*Commit, error) {
	if strings.ContainsRune(dir, 0) {
		return nil, &Error{Code: policy.CodeInvalidConfig, Stage: "commit", Message: "output directory must not contain NUL"}
	}
	cleaned := filepath.Clean(dir)
	if cleaned == "" || cleaned == "." {
		return nil, &Error{Code: policy.CodeInvalidConfig, Stage: "commit", Message: "output directory must not resolve to the source root"}
	}
	for _, target := range targets {
		if target.Name == "" || strings.ContainsAny(target.Name, "/\\") || target.Name == "." || target.Name == ".." {
			return nil, &Error{Code: policy.CodeInvalidConfig, Stage: "commit", Message: fmt.Sprintf("unsafe target name %q", target.Name)}
		}
	}
	return &Commit{
		writer:  writer,
		dir:     cleaned,
		targets: targets,
		lock:    filepath.Join(cleaned, ".si-generate.lock"),
	}, nil
}

// Prepare acquires the output-directory lock and pre-checks every target:
// the directory must exist (callers create it unless dry-running) and
// must not be a file or symlink; each target must be absent (unless force
// is given), a regular file, and never a symlink. Any violation aborts
// before a single byte is written.
func (commit *Commit) Prepare(force bool) error {
	info, err := commit.writer.Lstat(commit.dir)
	if err != nil {
		return &Error{Code: policy.CodeInvalidConfig, Stage: "commit", Message: fmt.Sprintf("output directory %q: %v", commit.dir, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return &Error{Code: policy.CodeUnsafeTarget, Stage: "commit", Message: fmt.Sprintf("output directory %q must not be a symlink", commit.dir)}
	}
	if !info.IsDir() {
		return &Error{Code: policy.CodeInvalidConfig, Stage: "commit", Message: fmt.Sprintf("output path %q is not a directory", commit.dir)}
	}

	lockFile, err := commit.writer.CreateExclusive(commit.lock, 0o644)
	if err != nil {
		return &Error{Code: policy.CodeOutputExists, Stage: "lock", Message: "another generation is already running in this directory"}
	}
	_ = lockFile.Close()

	for _, target := range commit.targets {
		path := filepath.Join(commit.dir, target.Name)
		info, err := commit.writer.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return &Error{Code: policy.CodeInvalidConfig, Stage: "commit", Message: fmt.Sprintf("inspect target %q: %v", target.Name, err)}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return &Error{Code: policy.CodeUnsafeTarget, Stage: "commit", Message: fmt.Sprintf("target %q is a symlink; refusing to follow it", target.Name)}
		}
		if !info.Mode().IsRegular() {
			return &Error{Code: policy.CodeUnsafeTarget, Stage: "commit", Message: fmt.Sprintf("target %q is not a regular file", target.Name)}
		}
		if !force {
			return &Error{Code: policy.CodeOutputExists, Stage: "commit", Message: fmt.Sprintf("target %q already exists; use --force to replace it", target.Name)}
		}
	}
	return nil
}

// Commit writes every target: an exclusive temporary file per target,
// full write/sync/close, backup of a pre-existing target, then an atomic
// rename. On the first failure the journal is rolled back and the error
// carries the rollback result.
func (commit *Commit) Commit() error {
	for index, target := range commit.targets {
		entry := journalEntry{
			name: target.Name,
			temp: filepath.Join(commit.dir, ".si-tmp-"+target.Name),
		}
		commit.journal = append(commit.journal, entry)
		current := &commit.journal[index]

		handle, err := commit.writer.CreateExclusive(current.temp, 0o644)
		if err != nil {
			return commit.fail(index, fmt.Errorf("create temporary file for %q: %w", target.Name, err))
		}
		if _, err := handle.Write(target.Contents); err != nil {
			_ = handle.Close()
			return commit.fail(index, fmt.Errorf("write temporary file for %q: %w", target.Name, err))
		}
		if err := handle.Sync(); err != nil {
			_ = handle.Close()
			return commit.fail(index, fmt.Errorf("sync temporary file for %q: %w", target.Name, err))
		}
		if err := handle.Close(); err != nil {
			return commit.fail(index, fmt.Errorf("close temporary file for %q: %w", target.Name, err))
		}

		destination := filepath.Join(commit.dir, target.Name)
		if _, err := commit.writer.Lstat(destination); err == nil {
			current.existedBefore = true
			current.backup = filepath.Join(commit.dir, ".si-backup-"+target.Name)
			if err := commit.writer.Rename(destination, current.backup); err != nil {
				return commit.fail(index, fmt.Errorf("backup %q: %w", target.Name, err))
			}
		}
		if err := commit.writer.Rename(current.temp, destination); err != nil {
			return commit.fail(index, fmt.Errorf("replace %q: %w", target.Name, err))
		}
		current.committed = true
	}
	return nil
}

// fail rolls back the journal after a failed commit and returns an error
// that preserves both the original failure and the rollback outcome.
func (commit *Commit) fail(failedIndex int, cause error) error {
	rollback := commit.rollback(failedIndex)
	return &Error{
		Code:      policy.CodeRenderError,
		Stage:     "commit",
		Message:   cause.Error(),
		Rollback:  rollback,
	}
}

// rollback restores pre-existing targets from their backups and removes
// newly created targets, in reverse commit order.
func (commit *Commit) rollback(failedIndex int) RollbackResult {
	result := RollbackResult{}
	for index := failedIndex - 1; index >= 0; index-- {
		entry := commit.journal[index]
		if entry.committed {
			destination := filepath.Join(commit.dir, entry.name)
			if entry.existedBefore {
				if err := commit.writer.Rename(entry.backup, destination); err != nil {
					result.Failed = append(result.Failed, entry.name)
				} else {
					result.Restored = append(result.Restored, entry.name)
				}
			} else {
				if err := commit.writer.Remove(destination); err != nil {
					result.Failed = append(result.Failed, entry.name)
				} else {
					result.Removed = append(result.Removed, entry.name)
				}
			}
		}
	}
	return result
}

// Cleanup removes every temporary and backup file, always called after a
// commit attempt. Errors are returned for the caller to merge, never
// swallowed.
func (commit *Commit) Cleanup() error {
	var failures []string
	for _, entry := range commit.journal {
		if entry.temp != "" {
			if err := commit.writer.Remove(entry.temp); err != nil && !os.IsNotExist(err) {
				failures = append(failures, entry.temp)
			}
		}
		if entry.backup != "" {
			if err := commit.writer.Remove(entry.backup); err != nil && !os.IsNotExist(err) {
				failures = append(failures, entry.backup)
			}
		}
	}
	if err := commit.writer.Remove(commit.lock); err != nil && !os.IsNotExist(err) {
		failures = append(failures, commit.lock)
	}
	if len(failures) > 0 {
		return fmt.Errorf("cleanup failed for: %s", strings.Join(failures, ", "))
	}
	return nil
}

// RollbackResult describes what a rollback did.
type RollbackResult struct {
	// Restored lists targets that existed before and were restored.
	Restored []string
	// Removed lists targets that did not exist before and were removed.
	Removed []string
	// Failed lists targets whose rollback failed.
	Failed []string
}

// Error is a commit-stage failure carrying a stable code, the stage and
// (after a failed multi-file commit) the rollback result.
type Error struct {
	Code     string
	Stage    string
	Message  string
	Rollback RollbackResult
}

// Error implements error. Messages never contain file contents.
func (failure *Error) Error() string {
	if len(failure.Rollback.Restored) == 0 && len(failure.Rollback.Removed) == 0 && len(failure.Rollback.Failed) == 0 {
		return fmt.Sprintf("%s: %s: %s", failure.Code, failure.Stage, failure.Message)
	}
	return fmt.Sprintf("%s: %s: %s; rollback restored %v, removed %v, failed %v",
		failure.Code, failure.Stage, failure.Message,
		failure.Rollback.Restored, failure.Rollback.Removed, failure.Rollback.Failed)
}
