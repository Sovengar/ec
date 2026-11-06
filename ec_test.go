package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const conflicted = `a
<<<<<<< HEAD
OURS
=======
THEIRS
>>>>>>> other
c
`

const diff3 = `a
<<<<<<< HEAD
OURS
||||||| base
BASE
=======
THEIRS
>>>>>>> other
c
`

func TestParseFile(t *testing.T) {
	f, err := ParseFile("f.txt", diff3)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Conflicts) != 1 {
		t.Fatalf("want 1 conflict, got %d", len(f.Conflicts))
	}
	c := f.Conflicts[0]
	if c.Ours != "OURS\n" || c.Base != "BASE\n" || c.Theirs != "THEIRS\n" {
		t.Fatalf("bad sides: %+v", c)
	}
	if !c.HasBase {
		t.Fatal("want base section detected")
	}
	if c.Start != 2 || c.End != 8 {
		t.Fatalf("bad range: %d..%d", c.Start, c.End)
	}
}

func TestParseClean(t *testing.T) {
	f, err := ParseFile("f.txt", "a\nb\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Conflicts) != 0 {
		t.Fatalf("want clean, got %d", len(f.Conflicts))
	}
}

func TestParseUnterminated(t *testing.T) {
	if _, err := ParseFile("f.txt", "<<<<<<< HEAD\na\n"); err == nil {
		t.Fatal("want error for unterminated block")
	}
}

func TestResolveStrategies(t *testing.T) {
	cases := map[Strategy]string{
		Ours:   "a\nOURS\nc\n",
		Theirs: "a\nTHEIRS\nc\n",
		Both:   "a\nOURS\nTHEIRS\nc\n",
		None:   "a\nc\n",
	}
	for s, want := range cases {
		f, err := ParseFile("f.txt", conflicted)
		if err != nil {
			t.Fatal(err)
		}
		got, n, err := f.Resolve(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if n != 1 || got != want {
			t.Fatalf("%s: got %q want %q", s, got, want)
		}
	}
}

func TestResolveMultipleBlocks(t *testing.T) {
	raw := conflicted + "x\n" + strings.Replace(conflicted, "a\n", "", 1)
	f, err := ParseFile("f.txt", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Conflicts) != 2 {
		t.Fatalf("want 2 conflicts, got %d", len(f.Conflicts))
	}
	got, _, err := f.Resolve(Ours)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, markerOurs) || strings.Count(got, "OURS") != 2 {
		t.Fatalf("bad resolution: %q", got)
	}
}

func TestBuildRequestContext(t *testing.T) {
	f, err := ParseFile("f.txt", diff3)
	if err != nil {
		t.Fatal(err)
	}
	req := BuildRequest([]*ConflictFile{f}, 1)
	if len(req.Files) != 1 || len(req.Files[0].Conflicts) != 1 {
		t.Fatalf("bad request: %+v", req)
	}
	hc := req.Files[0].Conflicts[0]
	if len(hc.Before) != 1 || hc.Before[0] != "a\n" {
		t.Fatalf("bad before context: %v", hc.Before)
	}
	if len(hc.After) != 1 || hc.After[0] != "c\n" {
		t.Fatalf("bad after context: %v", hc.After)
	}
}

func TestApplyResponseRejectsMarkers(t *testing.T) {
	f, err := ParseFile("f.txt", conflicted)
	if err != nil {
		t.Fatal(err)
	}
	resp := Response{Version: ProtocolVersion, Resolutions: []Resolution{
		{Path: "f.txt", ID: 1, Text: "<<<<<<< HEAD\noops\n=======\nx\n>>>>>>> y\n"},
	}}
	_, failed, err := ApplyResponse([]*ConflictFile{f}, resp, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) == 0 {
		t.Fatal("want rejection of marker text")
	}
}

func TestApplyResponseMissingID(t *testing.T) {
	f, err := ParseFile("f.txt", conflicted)
	if err != nil {
		t.Fatal(err)
	}
	resp := Response{Version: ProtocolVersion, Resolutions: []Resolution{
		{Path: "f.txt", ID: 7, Text: "x\n"},
	}}
	_, failed, err := ApplyResponse([]*ConflictFile{f}, resp, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) == 0 || !strings.Contains(failed[0], "unresolved") {
		t.Fatalf("want unresolved report, got %v", failed)
	}
}

// writeConflicted creates a temp file with markers.
func writeConflicted(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// captureStdout runs fn with stdout redirected and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	fn()
	_ = writer.Close()
	os.Stdout = previous
	return <-done
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = writer
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	fn()
	_ = writer.Close()
	os.Stderr = previous
	return <-done
}

func TestEndToEndApplyJSON(t *testing.T) {
	path := writeConflicted(t, conflicted)
	reply := filepath.Join(t.TempDir(), "reply.json")
	body := fmt.Sprintf(`{"version":"1","resolutions":[{"path":%q,"id":1,"text":%q}]}`, path, "MERGED\n")
	if err := os.WriteFile(reply, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code := run([]string{"--apply-json", reply, "--backup", path})
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\nMERGED\nc\n" {
		t.Fatalf("bad content: %q", got)
	}
	if _, err := os.Stat(path + ".ec.bak"); err != nil {
		t.Fatalf("want backup: %v", err)
	}
	if code := run([]string{"--check", path}); code != 0 {
		t.Fatalf("check after apply must pass, got %d", code)
	}
}

func TestEmitCLIPrintsVersionedHandoff(t *testing.T) {
	path := writeConflicted(t, conflicted)
	var code int
	out := captureStdout(t, func() {
		code = run([]string{"--emit", path})
	})
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	req, err := decodeRequest([]byte(out))
	if err != nil {
		t.Fatalf("emit output is not the handoff JSON: %v\n%s", err, out)
	}
	if req.Version != ProtocolVersion {
		t.Fatalf("version = %q", req.Version)
	}
	if len(req.Files) != 1 || req.Files[0].Conflicts[0].Ours != "OURS\n" {
		t.Fatalf("bad payload: %s", out)
	}
}

func TestAIRemovedIsAUsageError(t *testing.T) {
	path := writeConflicted(t, conflicted)
	var code int
	errOut := captureStderr(t, func() {
		code = run([]string{"--ai", "some-agent", path})
	})
	if code != 2 {
		t.Fatalf("want usage exit 2, got %d", code)
	}
	for _, want := range []string{"--ai was removed", `"a"`, "--emit", "--apply-json"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("error must name %q:\n%s", want, errOut)
		}
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != conflicted {
		t.Fatal("--ai must not touch the file")
	}
}

func TestEndToEndApplyAllCompat(t *testing.T) {
	dir := t.TempDir()
	merged := filepath.Join(dir, "merged.txt")
	if err := os.WriteFile(merged, []byte(conflicted), 0o644); err != nil {
		t.Fatal(err)
	}
	base, local, remote := filepath.Join(dir, "b"), filepath.Join(dir, "l"), filepath.Join(dir, "r")
	for p, s := range map[string]string{base: "a\nb\nc\n", local: "a\nOURS\nc\n", remote: "a\nTHEIRS\nc\n"} {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code := run([]string{"--apply-all", "theirs", base, local, remote, merged})
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	got, _ := os.ReadFile(merged)
	if string(got) != "a\nTHEIRS\nc\n" {
		t.Fatalf("bad content: %q", got)
	}
	if code := run([]string{"--check", merged}); code != 0 {
		t.Fatalf("check must pass, got %d", code)
	}
}

func TestCheckDirty(t *testing.T) {
	path := writeConflicted(t, conflicted)
	if code := run([]string{"--check", path}); code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestEmitJSON(t *testing.T) {
	path := writeConflicted(t, conflicted)
	files, err := LoadAll([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := EncodeRequest(BuildRequest(files, 2))
	if err != nil {
		t.Fatal(err)
	}
	req, err := decodeRequest(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Files) != 1 || req.Files[0].Conflicts[0].Ours != "OURS\n" {
		t.Fatalf("bad payload: %s", payload)
	}
}
