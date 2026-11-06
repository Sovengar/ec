package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sovengar/agentbridge"
)

func withTempCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous := cacheDir
	cacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { cacheDir = previous })
	return dir
}

func withLookPath(t *testing.T, installed ...string) {
	t.Helper()
	previous := lookPath
	lookPath = func(name string) (string, error) {
		for _, candidate := range installed {
			if candidate == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = previous })
}

func withEnv(t *testing.T, key, value string) {
	t.Helper()
	previous, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, previous)
			return
		}
		_ = os.Unsetenv(key)
	})
}

// fakeInvoker records the request and returns a canned answer.
type fakeInvoker struct {
	output string
	err    error
	got    agentbridge.Request
	calls  int
}

func (f *fakeInvoker) Invoke(_ context.Context, r agentbridge.Request) (agentbridge.Result, error) {
	f.calls++
	f.got = r
	if f.err != nil {
		return agentbridge.Result{}, f.err
	}
	return agentbridge.Result{Output: f.output}, nil
}

// fakeSource is the model catalog seam.
type fakeSource struct{ options agentbridge.Options }

func (f fakeSource) Options(context.Context, agentbridge.Agent) (agentbridge.Options, error) {
	return f.options, nil
}

func TestDetectListsOnlyInstalledHarnesses(t *testing.T) {
	withLookPath(t, "pi")
	b := New()
	names := b.Harnesses()
	if len(names) != 1 || names[0] != "pi" {
		t.Fatalf("Harnesses() = %v, want [pi]", names)
	}
	for _, name := range names {
		if name == "codex" {
			t.Fatal("codex is not installed and must not be listed")
		}
	}
}

func TestCapabilitiesRespectHarnessFlags(t *testing.T) {
	b := &Bridge{}
	if model, effort, headless := b.Capabilities("pi"); !model || !effort || !headless {
		t.Fatalf("pi caps = %v/%v/%v, want all true", model, effort, headless)
	}
	if _, effort, _ := b.Capabilities("jcode"); effort {
		t.Fatal("jcode has no reasoning-effort flag")
	}
	if _, _, headless := b.Capabilities("claude"); headless {
		t.Fatal("agentbridge declares no headless mode for claude")
	}
}

func TestOptionsComeFromTheCatalogSource(t *testing.T) {
	b := &Bridge{Agents: []agentbridge.Agent{{Name: "pi", Binary: "pi"}}, Models: fakeSource{
		options: agentbridge.Options{Models: []string{"p/m"}, Efforts: []string{"high"}},
	}}
	models, efforts := b.Options("pi")
	if len(models) != 1 || models[0] != "p/m" || len(efforts) != 1 || efforts[0] != "high" {
		t.Fatalf("Options(pi) = %v/%v", models, efforts)
	}
}

func TestOptionsAreEmptyWithoutACatalog(t *testing.T) {
	withTempCache(t)
	b := &Bridge{Agents: []agentbridge.Agent{{Name: "pi", Binary: "pi"}}, Models: fakeSource{}}
	models, efforts := b.Options("pi")
	if len(models) != 0 || len(efforts) != 0 {
		t.Fatalf("missing catalog must be empty, got %v/%v", models, efforts)
	}
	if models == nil && efforts == nil {
		// nil is the documented empty answer; the picker treats it as free text.
		return
	}
}

func TestDefaultsPreferEnvironmentThenRememberedThenDetected(t *testing.T) {
	withTempCache(t)
	b := &Bridge{Agents: []agentbridge.Agent{
		{Name: "codex", Binary: "codex"},
		{Name: "pi", Binary: "pi"},
	}}
	if harness, model, effort := b.Defaults(); harness != "codex" || model != "" || effort != "" {
		t.Fatalf("fresh defaults = %q/%q/%q, want codex with empty model/effort", harness, model, effort)
	}

	b.Remember("pi", "free", "high")
	if harness, model, effort := b.Defaults(); harness != "pi" || model != "free" || effort != "high" {
		t.Fatalf("remembered defaults = %q/%q/%q", harness, model, effort)
	}

	withEnv(t, EnvHarness, "codex")
	withEnv(t, EnvModel, "gpt")
	withEnv(t, EnvEffort, "low")
	if harness, model, effort := b.Defaults(); harness != "codex" || model != "gpt" || effort != "low" {
		t.Fatalf("env defaults = %q/%q/%q", harness, model, effort)
	}
}

func TestDefaultsFallBackWhenTheSavedHarnessIsGone(t *testing.T) {
	withTempCache(t)
	b := &Bridge{Agents: []agentbridge.Agent{{Name: "pi", Binary: "pi"}}}
	b.Remember("codex", "gpt", "high")

	harness, _, _ := b.Defaults()
	if harness != "pi" {
		t.Fatalf("uninstalled saved harness must fall back to a detected one, got %q", harness)
	}
}

func TestRememberSurvivesANewBridge(t *testing.T) {
	withTempCache(t)
	b := &Bridge{Agents: []agentbridge.Agent{{Name: "pi", Binary: "pi"}}}
	b.Remember("pi", "free", "high")

	next := &Bridge{Agents: []agentbridge.Agent{{Name: "pi", Binary: "pi"}}}
	harness, model, effort := next.Defaults()
	if harness != "pi" || model != "free" || effort != "high" {
		t.Fatalf("later run defaults = %q/%q/%q", harness, model, effort)
	}
}

func TestResolveExtractsAWrappedReply(t *testing.T) {
	invoker := &fakeInvoker{output: "Resolved:\n```json\n" +
		`{"version":"1","resolutions":[{"path":"f.txt","id":1,"text":"merged\n"}]}` + "\n```\n"}
	b := &Bridge{
		Agents:  []agentbridge.Agent{{Name: "pi", Binary: "pi"}},
		Invoker: invoker,
	}
	text, err := b.Resolve(context.Background(), "pi", "p/m", "high", "/tmp/f.txt", "ours\n", "base\n", "theirs\n")
	if err != nil {
		t.Fatal(err)
	}
	if text != "merged\n" {
		t.Fatalf("resolved text = %q", text)
	}
	if !strings.Contains(invoker.got.Prompt, `"version": "1"`) {
		t.Fatalf("prompt lost the payload: %s", invoker.got.Prompt)
	}
	if invoker.got.Model != "p/m" || invoker.got.Effort != "high" {
		t.Fatalf("model/effort not forwarded: %+v", invoker.got)
	}
}

func TestResolveRefusesMarkers(t *testing.T) {
	invoker := &fakeInvoker{output: `{"version":"1","resolutions":[{"path":"f.txt","id":1,"text":"<<<<<<< HEAD\noops\n=======\nx\n>>>>>>> y\n"}]}`}
	b := &Bridge{Agents: []agentbridge.Agent{{Name: "pi", Binary: "pi"}}, Invoker: invoker}
	if _, err := b.Resolve(context.Background(), "pi", "", "", "/tmp/f.txt", "o", "", "t"); err == nil {
		t.Fatal("marker-carrying reply must be refused")
	}
}

func TestResolveRefusesMissingConflict(t *testing.T) {
	invoker := &fakeInvoker{output: `{"version":"1","resolutions":[{"path":"f.txt","id":9,"text":"x\n"}]}`}
	b := &Bridge{Agents: []agentbridge.Agent{{Name: "pi", Binary: "pi"}}, Invoker: invoker}
	if _, err := b.Resolve(context.Background(), "pi", "", "", "/tmp/f.txt", "o", "", "t"); err == nil {
		t.Fatal("a reply without the conflict id must be refused")
	}
}

func TestResolveRefusesANonJSONReply(t *testing.T) {
	invoker := &fakeInvoker{output: "I am not sure how to resolve this."}
	b := &Bridge{Agents: []agentbridge.Agent{{Name: "pi", Binary: "pi"}}, Invoker: invoker}
	if _, err := b.Resolve(context.Background(), "pi", "", "", "/tmp/f.txt", "o", "", "t"); err == nil {
		t.Fatal("prose-only reply must be refused")
	}
}

func TestResolveNeverSpawnsWithoutAHeadlessMode(t *testing.T) {
	invoker := &fakeInvoker{output: "{}"}
	b := &Bridge{Agents: []agentbridge.Agent{{Name: "claude", Binary: "claude"}}, Invoker: invoker}
	_, err := b.Resolve(context.Background(), "claude", "", "", "/tmp/f.txt", "o", "", "t")
	if err == nil {
		t.Fatal("a harness without a headless mode must fail")
	}
	if !errors.Is(err, agentbridge.ErrNoHeadless) {
		t.Fatalf("want ErrNoHeadless, got %v", err)
	}
	if invoker.calls != 0 {
		t.Fatal("no process may start without a headless mode")
	}
}

func TestResolveSendsAnEmptyModelAsNoFlag(t *testing.T) {
	invoker := &fakeInvoker{output: `{"version":"1","resolutions":[{"path":"f.txt","id":1,"text":"merged\n"}]}`}
	b := &Bridge{Agents: []agentbridge.Agent{{Name: "pi", Binary: "pi"}}, Invoker: invoker}
	if _, err := b.Resolve(context.Background(), "pi", "", "", "/tmp/f.txt", "o", "", "t"); err != nil {
		t.Fatal(err)
	}
	argv, err := agentbridge.HeadlessArgs(invoker.got)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range argv {
		if arg == "--model" {
			t.Fatalf("an empty model must not add a flag: %v", argv)
		}
	}
}

func TestLastChoicePathIsUnderTheCacheDir(t *testing.T) {
	dir := withTempCache(t)
	path, err := lastChoicePath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Join(dir, "ec") {
		t.Fatalf("choice path = %s", path)
	}
}
