package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=ec", "GIT_AUTHOR_EMAIL=ec@example.com",
		"GIT_COMMITTER_NAME=ec", "GIT_COMMITTER_EMAIL=ec@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// initConflicted builds a repository with an unfinished merge.
func initConflicted(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "base")
	gitIn(t, dir, "checkout", "-qb", "side")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nSIDE\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-qam", "side")
	gitIn(t, dir, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nMAIN\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-qam", "main")
	cmd := exec.Command("git", "merge", "side")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=ec", "GIT_AUTHOR_EMAIL=ec@example.com",
		"GIT_COMMITTER_NAME=ec", "GIT_COMMITTER_EMAIL=ec@example.com",
	)
	if err := cmd.Run(); err == nil {
		t.Fatal("merge should conflict")
	}
	return dir
}

func TestMergeInProgressAndAbort(t *testing.T) {
	dir := initConflicted(t)
	active, err := MergeInProgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("want merge in progress")
	}
	if err := AbortMerge(dir, false); err != nil {
		t.Fatal(err)
	}
	active, err = MergeInProgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("merge still active after abort")
	}
	got, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "<<<<<<<") || string(got) != "a\nMAIN\nc\n" {
		t.Fatalf("worktree not restored: %q", got)
	}
	if _, conflicts, _ := CancelState(dir); conflicts != 0 {
		t.Fatalf("want 0 conflicts, got %d", conflicts)
	}
}

func TestAbortWithoutMerge(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	if err := AbortMerge(dir, false); err == nil {
		t.Fatal("want error when no merge is active")
	}
}

func TestAbortDryRun(t *testing.T) {
	dir := initConflicted(t)
	if err := AbortMerge(dir, true); err != nil {
		t.Fatal(err)
	}
	active, _ := MergeInProgress(dir)
	if !active {
		t.Fatal("dry-run must not abort")
	}
}
