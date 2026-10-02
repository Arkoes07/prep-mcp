// Package read answers queries over a vault: it scans the typed documents,
// filters and searches them, counts tags and builds the context bundle.
// It never changes a file.
package read

import (
	"time"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// Reader runs queries against one vault.
type Reader struct {
	v   *vault.Vault
	cfg *config.Config
	now func() time.Time
}

// New returns a Reader for v. now is the clock that sets "today".
func New(v *vault.Vault, cfg *config.Config, now func() time.Time) *Reader {
	return &Reader{v: v, cfg: cfg, now: now}
}

// Now returns the reader's clock, which is the server clock in production.
func (r *Reader) Now() time.Time { return r.now() }

// ReadDoc returns a file's cleaned path, bytes and hash. Any .md file in the
// vault can be read, including templates/, but not dot-folders.
func (r *Reader) ReadDoc(p string) (rel string, content []byte, hash string, err error) {
	rel, content, err = r.v.ReadFile(p)
	if err != nil {
		return "", nil, "", err
	}
	return rel, content, vault.Hash(content), nil
}
