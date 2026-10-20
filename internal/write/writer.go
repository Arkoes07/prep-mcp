// Package write changes files in a vault: create, append, patch frontmatter,
// edit text, replace. Each op, built next to its transform in its own file,
// works out a file's new content; the batch engine (batch.go) holds every op
// to the document rules, replaces each changed file atomically and makes at
// most one commit.
package write

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

// Writer applies safe changes to one vault. Writes are serialised, since tool
// calls may overlap; every write goes through the batch engine (batch.go, Apply
// and Batch), and every file is replaced atomically. A nil Git means no commits
// and no commit warnings: main passes nil when commits are off, and tests do,
// so they do not depend on git state.
type Writer struct {
	mu  sync.Mutex
	v   *vault.Vault
	cfg *config.Config
	git *Git
	now func() time.Time
	// beforeRename runs after a temp file is written and before it is renamed
	// into place, so tests can fail or interfere there. Production leaves it nil.
	beforeRename func(tmp string) error
}

// New builds a Writer. now is the clock for created and updated dates.
func New(v *vault.Vault, cfg *config.Config, g *Git, now func() time.Time) *Writer {
	return &Writer{v: v, cfg: cfg, git: g, now: now}
}

type Result struct {
	Path     string   `json:"path"`
	Hash     string   `json:"hash"`
	Warnings []string `json:"warnings"`
	Commit   string   `json:"commit,omitempty"`
}

// ConflictError means replace_doc saw a file that changed since it was read.
type ConflictError struct{ Path, Current string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s changed since you read it; its current hash is %s. Nothing was written: "+
		"re-read with read_doc, merge, retry with expected_hash set to the current hash", e.Path, e.Current)
}

// commit commits paths with msg when commits are on and anything was written,
// and returns the commit's hash and any warning.
func (w *Writer) commit(ctx context.Context, paths []string, msg string) (string, []string) {
	if w.git == nil || len(paths) == 0 {
		return "", nil
	}
	hash, warning := w.git.Commit(ctx, paths, msg)
	if warning == "" {
		return hash, nil
	}
	return hash, []string{warning}
}
