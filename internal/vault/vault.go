// Package vault is the core shared by reads and writes: confined access to a
// folder of markdown files, and parsing of their YAML frontmatter and tags.
//
// Every file access goes through an *os.Root, so the kernel confines it to
// the vault even if a path check is wrong. Resolve adds the readable errors.
//
// It imports no other package of this module: read and write both build on it.
package vault

import (
	"fmt"
	"os"
	"path/filepath"
)

// Vault is one root directory of markdown files.
type Vault struct {
	dir  string
	root *os.Root
}

// Open opens the vault at dir. The directory is resolved through symlinks once.
// The caller must Close the vault when done with it.
func Open(dir string) (*Vault, error) {
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
	return &Vault{dir: resolved, root: root}, nil
}

// Dir returns the vault's absolute directory, resolved through symlinks.
func (v *Vault) Dir() string { return v.dir }

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
