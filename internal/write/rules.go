package write

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// The document rules (CONTEXT.md): the frontmatter parses, the type is
// configured, the type's required fields have values and the tags are in the
// vocabulary. The plan holds every op to them once, against the file as the
// earlier ops leave it: an op fails only on a rule it breaks, and a rule the
// file already broke is a warning (ADR-0015).

// opRules is how one tool is held to the document rules: whether it may
// change the type, and what to do next when it breaks a rule.
type opRules struct {
	keepType   bool   // the op is a small change, so changing the type is a mistake
	fixParse   string // after an op leaves the frontmatter unparseable
	fixMissing string // after an op leaves required fields without a value
}

var (
	createRules = opRules{
		fixParse:   "report this as a bug",
		fixMissing: "add them to frontmatter and retry",
	}
	patchRules = opRules{
		fixParse:   "report this as a bug",
		fixMissing: "give them values with set instead of removing or emptying them",
	}
	// contentRules are for append_section and replace_doc, which take content.
	contentRules = opRules{
		fixParse:   "change content so the frontmatter stays valid",
		fixMissing: "keep them in the frontmatter",
	}
	editRules = opRules{
		keepType:   true,
		fixParse:   "change new so the frontmatter stays valid",
		fixMissing: "keep them in the frontmatter",
	}
)

// docState is what the rules see in one version of a file.
type docState struct {
	broken string     // why the frontmatter does not parse; "" if it does
	fm     *yaml.Node // the frontmatter mapping; nil without frontmatter
	typ    string     // "" for a plain file
	tags   []string
}

func stateOf(src []byte) docState {
	fm, err := vault.SplitFrontmatter(src)
	if err != nil {
		// The reason alone: the parser's advice to fix the file does not fit
		// an op that broke it, nor a write that already succeeded.
		var fe *vault.FrontmatterError
		if errors.As(err, &fe) {
			return docState{broken: fe.Reason}
		}
		return docState{broken: err.Error()}
	}
	if fm == nil {
		return docState{}
	}
	return docState{
		fm:   fm.Map,
		typ:  vault.Text(fm.Map, "type"),
		tags: vault.Values(fm.Map, "tags"),
	}
}

// verdict is what the rules find in one result: an error for each rule the
// op breaks, a warning for each rule the file already broke.
type verdict struct {
	errs     []error
	warnings []string
}

// check holds after, an op's result, to the document rules against before,
// the file as the earlier ops leave it (nil for a new file).
func (w *Writer) check(r opRules, before, after []byte) ([]string, error) {
	was, now := stateOf(before), stateOf(after)
	switch {
	case now.broken != "" && was.broken != "":
		return []string{now.broken + "; fix the frontmatter with edit_text"}, nil
	case now.broken != "":
		return nil, fmt.Errorf("this write breaks the frontmatter: %s; %s", now.broken, r.fixParse)
	case was.broken != "":
		// Nothing to compare against: check the result as if the file had been
		// so all along, so what the rules find is a warning and a write can
		// repair it.
		was = now
	}
	var v verdict
	w.checkType(&v, r, was, now)
	w.checkRequired(&v, r, was, now)
	w.checkTags(&v, was, now)
	if len(v.errs) > 0 {
		return nil, errors.Join(v.errs...)
	}
	return v.warnings, nil
}

// checkType holds the result to "the type is configured". A file without a
// type before and after is a plain file, which the rule leaves alone.
func (w *Writer) checkType(v *verdict, r opRules, was, now docState) {
	_, known := w.cfg.Types[now.typ]
	switch {
	case now.typ == was.typ:
		if now.typ != "" && !known {
			v.warnings = append(v.warnings, w.unknownType(now.typ)+"; set it with update_frontmatter")
		}
	case r.keepType:
		v.errs = append(v.errs, fmt.Errorf("this edit changes the type %q to %q; "+
			"keep the frontmatter's type line as it is", was.typ, now.typ))
	case now.typ == "":
		v.errs = append(v.errs, fmt.Errorf("this write removes the type %q, but every document needs one: "+
			"keep it, or set it to one of the configured types: %s", was.typ, w.cfg.TypeList()))
	case !known:
		v.errs = append(v.errs, errors.New(w.unknownType(now.typ)))
	}
}

// unknownType is the type rule's text for a type the config does not define.
func (w *Writer) unknownType(typ string) string {
	return fmt.Sprintf("unknown type %q; use one of the configured types: %s", typ, w.cfg.TypeList())
}

// checkRequired holds the result to "the type's required fields have
// values". A changed type is checked like a new document: every field it
// misses is the op's doing.
func (w *Writer) checkRequired(v *verdict, r opRules, was, now docState) {
	spec, ok := w.cfg.Types[now.typ]
	if !ok {
		return
	}
	var already, broke []string
	if was.typ == now.typ {
		already = missingFields(spec.Required, was.fm)
	}
	missing := missingFields(spec.Required, now.fm)
	for _, f := range missing {
		if !slices.Contains(already, f) {
			broke = append(broke, f)
		}
	}
	if len(broke) > 0 {
		v.errs = append(v.errs, fmt.Errorf(
			"missing required fields for %s: %s; %s",
			now.typ,
			strings.Join(broke, ", "),
			r.fixMissing,
		))
		return
	}
	if len(missing) > 0 {
		v.warnings = append(v.warnings, fmt.Sprintf("missing required fields for %s: %s; "+
			"give them values with update_frontmatter", now.typ, strings.Join(missing, ", ")))
	}
}

// checkTags holds the result to "the tags are in the vocabulary", naming the
// closest known tag. Without a vocabulary there is nothing to check against.
func (w *Writer) checkTags(v *verdict, was, now docState) {
	if len(now.tags) == 0 {
		return
	}
	added := slices.ContainsFunc(now.tags, func(t string) bool { return !slices.Contains(was.tags, t) })
	voc, err := w.v.Vocabulary(w.cfg.TagsFile)
	switch {
	case err != nil && added:
		v.errs = append(v.errs, err)
		return
	case err != nil:
		// It cannot vouch for an added tag, but it does not block a write that adds none.
		v.warnings = append(v.warnings, err.Error())
		return
	case len(voc.Tags()) == 0:
		return
	}
	for _, t := range now.tags {
		if voc.Known(t) {
			continue
		}
		found := fmt.Sprintf("unknown tag %q; closest known tag: %q", t, voc.Closest(t))
		if slices.Contains(was.tags, t) {
			v.warnings = append(v.warnings, found+". Replace it with a known tag, or add it to "+w.cfg.TagsFile)
			continue
		}
		v.errs = append(v.errs, fmt.Errorf("%s. Use a known tag (list_tags shows them all), or add it to %s first",
			found, w.cfg.TagsFile))
	}
}

func missingFields(required []string, m *yaml.Node) []string {
	var out []string
	for _, r := range required {
		if !vault.HasValue(m, r) {
			out = append(out, r)
		}
	}
	return out
}
