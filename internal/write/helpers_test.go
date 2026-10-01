package write_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

// today is testvault.Day as the writer stamps it into created/updated.
const today = "2026-01-14"

// env is a temp copy of examples/vault with a Writer on it.
type env struct {
	root string
	v    *vault.Vault
	cfg  *config.Config
	w    *write.Writer
}

// newEnv copies the example vault and builds a Writer with the frozen test
// clock. The Git is nil, so no commit is attempted and no commit warning is
// added: tests can assert "no warnings" without depending on git state.
func newEnv(t testing.TB) *env {
	t.Helper()
	root := testvault.Copy(t)
	cfg, err := config.Load(filepath.Join(root, "prep.config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	v, err := vault.Open(root, cfg)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	testvault.CloseOnCleanup(t, v)
	return &env{root: root, v: v, cfg: cfg, w: write.New(v, cfg, nil, testvault.Clock())}
}

func (e *env) abs(rel string) string { return filepath.Join(e.root, filepath.FromSlash(rel)) }

func (e *env) read(t testing.TB, rel string) string {
	t.Helper()
	b, err := os.ReadFile(e.abs(rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func (e *env) exists(rel string) bool {
	_, err := os.Lstat(e.abs(rel))
	return err == nil
}

// snapshot maps every file under root to its content. Comparing two snapshots
// proves a failed write changed nothing and left no temp file behind.
func snapshot(t testing.TB, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return out
}

func sameSnapshot(t testing.TB, before, after map[string]string) {
	t.Helper()
	for k, v := range before {
		if got, ok := after[k]; !ok {
			t.Errorf("%s disappeared", k)
		} else if got != v {
			t.Errorf("%s changed:\n--- before\n%s\n--- after\n%s", k, v, got)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("%s appeared", k)
		}
	}
}

// leftovers lists temp files the atomic write should never leave behind.
func leftovers(t testing.TB, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), ".prep-tmp-") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// node decodes a JSON object into an ordered mapping node, the way the
// server hands frontmatter to the writer.
func node(t testing.TB, js string) *yaml.Node {
	t.Helper()
	n, err := write.JSONToNode(json.RawMessage(js))
	if err != nil {
		t.Fatalf("JSONToNode(%s): %v", js, err)
	}
	return n
}

// mustContain fails unless err is non-nil and mentions every fragment.
func mustContain(t testing.TB, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one containing %q", fragments)
	}
	for _, f := range fragments {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("error %q does not contain %q", err, f)
		}
	}
}

// lines are counted on '\n' so a CRLF file keeps its '\r'.
func countLines(s, line string) int {
	n := 0
	for l := range strings.SplitSeq(s, "\n") {
		if strings.TrimRight(l, " \r") == line {
			n++
		}
	}
	return n
}
