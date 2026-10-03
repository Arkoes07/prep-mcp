package read_test

import (
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/read"
	"github.com/Arkoes07/prep-mcp/internal/testvault"
)

// openAt opens the vault at dir with the config found in it and returns a
// Reader on the frozen test clock.
func openAt(t testing.TB, dir string) *read.Reader {
	t.Helper()
	cfg, v := testvault.Open(t, dir)
	return read.New(v, cfg, testvault.Clock())
}

func copyOpen(t testing.TB) (string, *read.Reader) {
	t.Helper()
	root := testvault.Copy(t)
	return root, openAt(t, root)
}

func paths(docs []*read.Doc) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.Path)
	}
	return out
}
