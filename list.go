package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ListConflicts returns unmerged paths reported by git.
func ListConflicts() ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var paths []string
	for _, line := range strings.Split(out.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

// runListOnly prints conflicted files without prompting.
func runListOnly() int {
	paths, err := ListConflicts()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	if len(paths) == 0 {
		fmt.Println("ec: no conflicts")
		return 0
	}
	for _, p := range paths {
		fmt.Println(p)
	}
	return 1
}

// runList prints conflicted files and offers a choice on a terminal.
func runList() int {
	paths, err := ListConflicts()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	if len(paths) == 0 {
		fmt.Println("ec: no conflicts")
		return 0
	}
	for i, p := range paths {
		fmt.Printf("%2d  %s\n", i+1, p)
	}
	if !isTerminal(os.Stdin) {
		return 1
	}
	fmt.Print("select [1-" + strconv.Itoa(len(paths)) + "]: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(paths) {
		fmt.Fprintln(os.Stderr, "ec: invalid selection")
		return 2
	}
	return runCheck(paths[n-1 : n])
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
