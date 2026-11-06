package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// git runs one git command and returns trimmed stdout.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(out.String()), errors.New(msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// MergeInProgress reports whether the repository has an unfinished merge.
func MergeInProgress(dir string) (bool, error) {
	if _, err := git(dir, "rev-parse", "--git-dir"); err != nil {
		return false, fmt.Errorf("not a git repository")
	}
	out, err := git(dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	if err != nil || out == "" {
		return false, nil
	}
	return true, nil
}

// AbortMerge cancels an in-progress merge and restores the pre-merge state.
func AbortMerge(dir string, dryRun bool) error {
	active, err := MergeInProgress(dir)
	if err != nil {
		return err
	}
	if !active {
		return errors.New("no merge in progress")
	}
	if dryRun {
		return nil
	}
	if _, err := git(dir, "merge", "--abort"); err == nil {
		return nil
	}
	// Older or odd states: fall back to a hard reset of the merge.
	if _, err := git(dir, "reset", "--merge"); err != nil {
		return fmt.Errorf("abort merge: %w", err)
	}
	return nil
}

// CancelState reports remaining merge artifacts after an abort attempt.
func CancelState(dir string) (active bool, conflicts int, err error) {
	active, err = MergeInProgress(dir)
	if err != nil {
		return false, 0, err
	}
	out, err := git(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return active, 0, nil
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			conflicts++
		}
	}
	return active, conflicts, nil
}
