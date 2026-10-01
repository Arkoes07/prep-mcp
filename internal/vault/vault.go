// Package vault reads a folder of markdown files with YAML frontmatter.
//
// Every file access goes through an *os.Root, so the kernel confines it to
// the vault even if a path check is wrong. Resolve adds the readable errors.
package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/config"
)

// Vault is one root directory of markdown files.
type Vault struct {
	dir  string
	root *os.Root
	cfg  *config.Config
	now  func() time.Time
}

// Open opens the vault at dir. The directory is resolved through symlinks once.
// A nil now means the system clock; a nil cfg means an empty config. The
// caller must Close the vault when done with it.
func Open(dir string, cfg *config.Config, now func() time.Time) (*Vault, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("vault root %q: %w (pass an absolute --root)", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("vault root %q: %w (check --root or PREP_ROOT)", dir, err)
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, fmt.Errorf("vault root %q: %w (check --root or PREP_ROOT)", dir, err)
	}
	if cfg == nil {
		if cfg, err = config.Parse(nil); err != nil {
			return nil, errors.Join(fmt.Errorf("empty default config: %w", err), root.Close())
		}
	}
	if now == nil {
		now = time.Now
	}
	return &Vault{dir: resolved, root: root, cfg: cfg, now: now}, nil
}

// Dir returns the vault's absolute directory, resolved through symlinks.
func (v *Vault) Dir() string { return v.dir }

// Now returns the vault's clock, which is the server clock in production.
func (v *Vault) Now() time.Time { return v.now() }

// Root returns the confining root. All file I/O must go through it.
func (v *Vault) Root() *os.Root { return v.root }

// Close releases the vault's directory handle. The vault must not be used
// afterwards.
func (v *Vault) Close() error {
	if err := v.root.Close(); err != nil {
		return fmt.Errorf("close vault %s: %w", v.dir, err)
	}
	return nil
}
