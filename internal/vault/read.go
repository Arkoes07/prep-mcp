package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
)

// ReadFile returns a file's cleaned path and bytes. Any .md file in the vault
// can be read, including templates/ so the model can follow a template's
// structure, but not dot-folders.
func (v *Vault) ReadFile(p string) (rel string, content []byte, err error) {
	rel, err = v.Resolve(p, AccessRead)
	if err != nil {
		return "", nil, err
	}
	content, err = v.root.ReadFile(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, notFound(rel)
		}
		return "", nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return rel, content, nil
}

// Hash is the lowercase hex sha256 of b. replace_doc compares it.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
