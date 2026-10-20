package write

// SetBeforeRename sets the hook that runs between writing a temp file and
// renaming it into place.
func (w *Writer) SetBeforeRename(f func(tmp string) error) { w.beforeRename = f }

var InsertSection = insertSection

func ApplyPatch(src []byte, p Patch, today string, stamp bool) ([]byte, []string, error) {
	out, _, warnings, err := applyPatch(parse(src), p, today, stamp)
	return out, warnings, err
}
