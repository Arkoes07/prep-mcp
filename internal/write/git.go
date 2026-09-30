package write

import (
	"context"
	"time"
)

// Git commits written files. In M2 it is a no-op; M3 implements it.
type Git struct {
	Bin, Author string
	Timeout     time.Duration
}

// Commit commits rel in dir. It never fails a write: problems come back as warning.
func (g *Git) Commit(ctx context.Context, dir, rel, msg string) (hash, warning string) {
	return "", ""
}
