// Package commit implements the safe, offline file commit stage of
// `si generate` (Phase 1, P1-14).
//
// Guarantees: single-file atomic replacement (write to an exclusive
// temporary file, fsync, then rename); a multi-file journal that backs up
// pre-existing targets and restores them or removes newly created targets
// when a later rename fails; symlink and non-regular target rejection;
// and a per-output-directory exclusive lock so concurrent generations
// fail fast instead of overwriting each other.
//
// The FileWriter interface abstracts every filesystem operation, so the
// pipeline is testable without real disk writes and every fault point
// (open, write, sync, close, rename, rollback, cleanup) is injectable.
//
// Scope limitation, documented for consumers: the commit guarantees
// per-file atomicity and in-process rollback only. A process killed by
// force, kernel crash or power loss may leave a mixed set of complete old
// and new files, but never a truncated renamed target.
package commit
