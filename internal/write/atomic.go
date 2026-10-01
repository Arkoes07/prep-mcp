package write

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// Atomic file writes: every change is staged in a temp file next to its
// target and renamed into place, after a last check that the target is still
// what the caller expects. A check that refuses returns the error the caller
// acts on (*ConflictError or *ExistsError), and nothing is written.

// beforeRename is a test seam: it runs after the temp file is written and
// before it is renamed into place. Production leaves it nil.
var beforeRename func(tmp string) error

func (w *Writer) readExisting(rel string) ([]byte, error) {
	b, err := w.v.Root().ReadFile(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("not found: %s (use create_doc to make a new document)", rel)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return b, nil
}

// replaceFile atomically replaces an existing file, provided it still has the
// content with hash oldHash. If it does not, nothing is written and the error
// is a *ConflictError carrying the file's current hash.
func (w *Writer) replaceFile(rel string, data []byte, oldHash string) error {
	mode := fs.FileMode(0o644)
	if fi, err := w.v.Root().Stat(rel); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := w.stage(rel, data, mode)
	if err != nil {
		return err
	}
	return w.finish(rel, tmp, func() error {
		now, err := w.v.Root().ReadFile(rel)
		if err != nil {
			return fmt.Errorf("cannot write %s: it disappeared while it was being edited (%w); re-read it and retry", rel, err)
		}
		if cur := vault.Hash(now); cur != oldHash {
			return &ConflictError{Path: rel, Current: cur}
		}
		return nil
	})
}

// createFile writes a new file and refuses, with an *ExistsError, to replace
// an existing one.
func (w *Writer) createFile(rel string, data []byte) error {
	absent := func() error {
		_, err := w.v.Root().Lstat(rel)
		switch {
		case err == nil:
			return &ExistsError{Path: rel}
		case errors.Is(err, fs.ErrNotExist):
			return nil
		}
		return fmt.Errorf("cannot check %s: %w", rel, err)
	}
	if err := absent(); err != nil {
		return err
	}
	tmp, err := w.stage(rel, data, 0o644)
	if err != nil {
		return err
	}
	return w.finish(rel, tmp, absent)
}

// stage writes data to a new temp file next to rel and returns its name. The
// temp file gets the final mode, so nothing is ever group- or world-writable by accident.
func (w *Writer) stage(rel string, data []byte, mode fs.FileMode) (string, error) {
	root := w.v.Root()
	dir := path.Dir(rel)
	if dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("cannot create the folder for %s: %w; check permissions", rel, err)
		}
	}
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", rel, err)
	}
	tmp := path.Join(dir, ".prep-tmp-"+hex.EncodeToString(suffix[:]))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("cannot write %s: %w; check the folder's permissions", rel, err)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = root.Chmod(tmp, mode)
	}
	if werr != nil {
		root.Remove(tmp)
		return "", fmt.Errorf("cannot write %s: %w", rel, werr)
	}
	return tmp, nil
}

// finish runs the test seam and check, then renames tmp over rel. On any
// failure, a refusing check included, the temp file is removed and the error
// returned.
func (w *Writer) finish(rel, tmp string, check func() error) error {
	root := w.v.Root()
	if beforeRename != nil {
		if err := beforeRename(tmp); err != nil {
			root.Remove(tmp)
			return fmt.Errorf("cannot write %s: %w", rel, err)
		}
	}
	if err := check(); err != nil {
		root.Remove(tmp)
		return err
	}
	if err := root.Rename(tmp, rel); err != nil {
		root.Remove(tmp)
		return fmt.Errorf("cannot write %s: %w", rel, err)
	}
	return nil
}
