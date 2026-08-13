package commit

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
)

// faultWriter wraps OSFileWriter and injects failures at configurable
// points.
type faultWriter struct {
	FileWriter
	// failCreate/failWrite/failSync/failClose fail the matching operation
	// when non-nil.
	failCreate error
	failWrite  error
	failSync   error
	failClose  error
	// failRenames lists rename call indexes (1-based) that must fail.
	failRenames []int
	renameCount int
	// failRemoves lists remove call indexes that must fail.
	failRemoves []int
	removeCount int
	created     []string
}

func (writer *faultWriter) CreateExclusive(path string, perm os.FileMode) (WriteCloserSyncer, error) {
	writer.created = append(writer.created, path)
	// The lock file must never fail; faults target the temporary files
	// written during Commit.
	if strings.Contains(path, ".si-generate.lock") {
		return writer.FileWriter.CreateExclusive(path, perm)
	}
	if writer.failCreate != nil {
		return nil, writer.failCreate
	}
	handle, err := writer.FileWriter.CreateExclusive(path, perm)
	if err != nil {
		return nil, err
	}
	return writer.wrap(handle), nil
}

func (writer *faultWriter) Rename(oldPath, newPath string) error {
	writer.renameCount++
	if slices.Contains(writer.failRenames, writer.renameCount) {
		return errors.New("injected rename failure")
	}
	return writer.FileWriter.Rename(oldPath, newPath)
}

func (writer *faultWriter) Remove(path string) error {
	writer.removeCount++
	if slices.Contains(writer.failRemoves, writer.removeCount) {
		return errors.New("injected remove failure")
	}
	return writer.FileWriter.Remove(path)
}

// failingHandle wraps a real handle and injects write/sync/close errors.
type failingHandle struct {
	WriteCloserSyncer
	writeErr error
	syncErr  error
	closeErr error
}

func (handle *failingHandle) Write(contents []byte) (int, error) {
	if handle.writeErr != nil {
		return 0, handle.writeErr
	}
	return handle.WriteCloserSyncer.Write(contents)
}

func (handle *failingHandle) Sync() error {
	if handle.syncErr != nil {
		return handle.syncErr
	}
	return handle.WriteCloserSyncer.Sync()
}

func (handle *failingHandle) Close() error {
	if handle.closeErr != nil {
		return handle.closeErr
	}
	return handle.WriteCloserSyncer.Close()
}

func (writer *faultWriter) wrap(handle WriteCloserSyncer) WriteCloserSyncer {
	if writer.failWrite == nil && writer.failSync == nil && writer.failClose == nil {
		return handle
	}
	return &failingHandle{WriteCloserSyncer: handle, writeErr: writer.failWrite, syncErr: writer.failSync, closeErr: writer.failClose}
}

func testTargets() []Target {
	return []Target{
		{Name: "metrics.yaml", Contents: []byte("metrics: []\n")},
		{Name: "otel.yaml", Contents: []byte("spans: []\n")},
		{Name: "logging.yaml", Contents: []byte("events: []\n")},
	}
}

func setupDir(t *testing.T) (string, FileWriter) {
	t.Helper()
	dir := t.TempDir()
	return dir, OSFileWriter{}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(contents) != want {
		t.Fatalf("%s = %q, want %q", path, contents, want)
	}
}

func assertAbsent(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			t.Errorf("%s must not exist", path)
		}
	}
}

// TestCommitNewTargets: absent targets are created atomically with no
// leftover temporary, backup or lock files.
func TestCommitNewTargets(t *testing.T) {
	dir, writer := setupDir(t)
	generation, err := New(writer, dir, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := generation.Prepare(false); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if err := generation.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if err := generation.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	for _, target := range testTargets() {
		assertFile(t, filepath.Join(dir, target.Name), string(target.Contents))
	}
	assertAbsent(t,
		filepath.Join(dir, ".si-generate.lock"),
		filepath.Join(dir, ".si-tmp-metrics.yaml"),
		filepath.Join(dir, ".si-backup-metrics.yaml"))
}

// TestCommitForceReplacesExisting: pre-existing targets are backed up and
// replaced; backups are cleaned up.
func TestCommitForceReplacesExisting(t *testing.T) {
	dir, writer := setupDir(t)
	for _, target := range testTargets() {
		if err := os.WriteFile(filepath.Join(dir, target.Name), []byte("old"), 0o644); err != nil {
			t.Fatalf("seed target: %v", err)
		}
	}
	generation, err := New(writer, dir, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := generation.Prepare(true); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if err := generation.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if err := generation.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	for _, target := range testTargets() {
		assertFile(t, filepath.Join(dir, target.Name), string(target.Contents))
	}
	assertAbsent(t, filepath.Join(dir, ".si-backup-metrics.yaml"))
}

// TestPrepareRejectsExistingWithoutForce: an existing target without
// --force fails with GEN_OUTPUT_EXISTS and changes nothing.
func TestPrepareRejectsExistingWithoutForce(t *testing.T) {
	dir, writer := setupDir(t)
	if err := os.WriteFile(filepath.Join(dir, "metrics.yaml"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	generation, err := New(writer, dir, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = generation.Prepare(false)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != policy.CodeOutputExists {
		t.Fatalf("error = %v, want GEN_OUTPUT_EXISTS", err)
	}
	if !strings.Contains(err.Error(), "metrics.yaml") {
		t.Errorf("error %q lacks the target name", err.Error())
	}
	assertFile(t, filepath.Join(dir, "metrics.yaml"), "old")
	assertAbsent(t, filepath.Join(dir, "otel.yaml"), filepath.Join(dir, "logging.yaml"))
}

// TestPrepareRejectsSymlinkTarget: a symlink target fails with
// GEN_UNSAFE_TARGET and the external file stays untouched.
func TestPrepareRejectsSymlinkTarget(t *testing.T) {
	dir, writer := setupDir(t)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("external"), 0o644); err != nil {
		t.Fatalf("seed outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "metrics.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	generation, err := New(writer, dir, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = generation.Prepare(true)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != policy.CodeUnsafeTarget {
		t.Fatalf("error = %v, want GEN_UNSAFE_TARGET", err)
	}
	assertFile(t, outside, "external")
}

// TestPrepareRejectsSymlinkDir: the output directory itself must not be a
// symlink.
func TestPrepareRejectsSymlinkDir(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	generation, err := New(OSFileWriter{}, link, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = generation.Prepare(false)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != policy.CodeUnsafeTarget {
		t.Fatalf("error = %v, want GEN_UNSAFE_TARGET", err)
	}
}

// TestPrepareRejectsFileAsDir: the output path must be a directory.
func TestPrepareRejectsFileAsDir(t *testing.T) {
	dir, writer := setupDir(t)
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	generation, err := New(writer, file, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := generation.Prepare(false); err == nil {
		t.Fatal("file as output directory must fail")
	}
}

// TestRollbackRestoresExistingAC7: when the second rename fails and all
// targets existed before, the first target is restored, temps are
// cleaned, and the error carries the rollback result.
func TestRollbackRestoresExistingAC7(t *testing.T) {
	dir := t.TempDir()
	for _, target := range testTargets() {
		if err := os.WriteFile(filepath.Join(dir, target.Name), []byte("old-"+target.Name), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	writer := &faultWriter{FileWriter: OSFileWriter{}, failRenames: []int{3}} // backup1, replace1, backup2, ...
	generation, err := New(writer, dir, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := generation.Prepare(true); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	err = generation.Commit()
	if err == nil {
		t.Fatal("commit must fail on the injected rename failure")
	}
	rollbackErr := generation.Cleanup()
	if rollbackErr != nil {
		t.Fatalf("cleanup after rollback: %v", rollbackErr)
	}
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type = %T, want *commit.Error", err)
	}
	if len(failure.Rollback.Restored) != 1 || failure.Rollback.Restored[0] != "metrics.yaml" {
		t.Errorf("rollback restored = %v, want [metrics.yaml]", failure.Rollback.Restored)
	}
	if !strings.Contains(err.Error(), "rollback") {
		t.Errorf("error %q lacks the rollback result", err.Error())
	}
	// Target 1 restored to the original bytes; no truncation anywhere.
	assertFile(t, filepath.Join(dir, "metrics.yaml"), "old-metrics.yaml")
	assertFile(t, filepath.Join(dir, "otel.yaml"), "old-otel.yaml")
	assertFile(t, filepath.Join(dir, "logging.yaml"), "old-logging.yaml")
	assertAbsent(t,
		filepath.Join(dir, ".si-tmp-metrics.yaml"),
		filepath.Join(dir, ".si-backup-metrics.yaml"),
		filepath.Join(dir, ".si-generate.lock"))
}

// TestRollbackRemovesNewTargetsAC7: when the second rename fails and no
// target existed before, the first target is removed again.
func TestRollbackRemovesNewTargetsAC7(t *testing.T) {
	dir := t.TempDir()
	writer := &faultWriter{FileWriter: OSFileWriter{}, failRenames: []int{2}} // replace1, replace2 fails
	generation, err := New(writer, dir, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := generation.Prepare(false); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	err = generation.Commit()
	if err == nil {
		t.Fatal("commit must fail on the injected rename failure")
	}
	_ = generation.Cleanup()
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type = %T", err)
	}
	if len(failure.Rollback.Removed) != 1 || failure.Rollback.Removed[0] != "metrics.yaml" {
		t.Errorf("rollback removed = %v, want [metrics.yaml]", failure.Rollback.Removed)
	}
	assertAbsent(t,
		filepath.Join(dir, "metrics.yaml"),
		filepath.Join(dir, "otel.yaml"),
		filepath.Join(dir, "logging.yaml"),
		filepath.Join(dir, ".si-tmp-metrics.yaml"),
		filepath.Join(dir, ".si-generate.lock"))
}

// TestCommitFaultPoints: failures at create, write, sync and close all
// abort without leaving targets or temporaries behind.
func TestCommitFaultPoints(t *testing.T) {
	faults := []struct {
		name   string
		mutate func(*faultWriter)
	}{
		{"create", func(writer *faultWriter) { writer.failCreate = errors.New("create failed") }},
		{"write", func(writer *faultWriter) { writer.failWrite = errors.New("write failed") }},
		{"sync", func(writer *faultWriter) { writer.failSync = errors.New("sync failed") }},
		{"close", func(writer *faultWriter) { writer.failClose = errors.New("close failed") }},
	}
	for _, fault := range faults {
		t.Run(fault.name, func(t *testing.T) {
			dir := t.TempDir()
			writer := &faultWriter{FileWriter: OSFileWriter{}}
			fault.mutate(writer)
			generation, err := New(writer, dir, testTargets())
			if err != nil {
				t.Fatalf("New failed: %v", err)
			}
			if err := generation.Prepare(false); err != nil {
				t.Fatalf("Prepare failed: %v", err)
			}
			if err := generation.Commit(); err == nil {
				t.Fatal("commit must fail on the injected fault")
			}
			_ = generation.Cleanup()
			assertAbsent(t,
				filepath.Join(dir, "metrics.yaml"),
				filepath.Join(dir, "otel.yaml"),
				filepath.Join(dir, "logging.yaml"),
				filepath.Join(dir, ".si-generate.lock"))
		})
	}
}

// TestLockConflict: a second Commit cannot Prepare while the first holds
// the lock; the failure is fast, not a wait.
func TestLockConflict(t *testing.T) {
	dir, writer := setupDir(t)
	first, err := New(writer, dir, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := first.Prepare(false); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	second, err := New(writer, dir, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = second.Prepare(false)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != policy.CodeOutputExists {
		t.Fatalf("lock conflict error = %v, want GEN_OUTPUT_EXISTS", err)
	}
	if err := first.Cleanup(); err != nil {
		t.Fatalf("first cleanup: %v", err)
	}
	if err := second.Prepare(false); err != nil {
		t.Fatalf("prepare after lock release must succeed: %v", err)
	}
	if err := second.Cleanup(); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
}

// TestCleanupErrorsAreReported: cleanup failures are returned, never
// swallowed.
func TestCleanupErrorsAreReported(t *testing.T) {
	dir := t.TempDir()
	writer := &faultWriter{FileWriter: OSFileWriter{}, failRemoves: []int{1}}
	generation, err := New(writer, dir, testTargets())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := generation.Prepare(false); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if err := generation.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if err := generation.Cleanup(); err == nil {
		t.Fatal("cleanup failure must be reported")
	}
}

// TestNewRejectsUnsafePaths: NUL bytes, empty directories and unsafe
// target names are rejected before any filesystem access.
func TestNewRejectsUnsafePaths(t *testing.T) {
	writer := OSFileWriter{}
	if _, err := New(writer, "gen\x00erate", testTargets()); err == nil {
		t.Error("NUL directory must be rejected")
	}
	if _, err := New(writer, ".", testTargets()); err == nil {
		t.Error("source-root directory must be rejected")
	}
	if _, err := New(writer, "out", []Target{{Name: "../escape.yaml", Contents: []byte("x")}}); err == nil {
		t.Error("path traversal target must be rejected")
	}
}
