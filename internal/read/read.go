// Package read answers queries over a vault: it scans the typed documents,
// filters and searches them, counts tags and builds the context bundle.
// It never changes a file.
package read

import (
	"time"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

type Reader struct {
	v   *vault.Vault
	cfg *config.Config
	now func() time.Time
}

// New returns a Reader for v. now is the clock that sets "today".
func New(v *vault.Vault, cfg *config.Config, now func() time.Time) *Reader {
	return &Reader{v: v, cfg: cfg, now: now}
}

func (r *Reader) Now() time.Time { return r.now() }

// ReadDoc reads the file at a vault-relative path, or the one file a
// [[wikilink]] or bare name points to.
func (r *Reader) ReadDoc(p string) (rel string, content []byte, hash string, err error) {
	if target, isLink := parseLink(p); isLink {
		if p, err = r.resolveLink(target); err != nil {
			return "", nil, "", err
		}
	}
	rel, content, err = r.v.ReadFile(p)
	if err != nil {
		return "", nil, "", err
	}
	return rel, content, vault.Hash(content), nil
}
