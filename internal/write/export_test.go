package write

// SetBeforeRename sets the hook that runs between writing a temp file and
// renaming it into place.
func (w *Writer) SetBeforeRename(f func(tmp string) error) { w.beforeRename = f }

var (
	ApplyPatch    = applyPatch
	InsertSection = insertSection
)
