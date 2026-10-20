package write

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/arkoes07/prep-mcp/internal/document"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

// The document rules (GLOSSARY.md): the frontmatter parses, the type is
// configured, the type's required fields have values, the date field holds a
// date and the tags are in the vocabulary. The plan holds every op to them
// once, against the file and the tags file as the earlier ops leave them: an
// op fails only on a rule it breaks, and a rule the file already broke is a
// warning.
//
// Holding every result to every rule would block all writes, appends
// included, to a file a human left without a required field or with an old
// tag; this way a write can also repair one. The cost: a file can keep a
// broken rule indefinitely, warned about on every write. Rejected: checking
// each file once at the end of a batch, which would blame a file, not an op.

// opRules is how one op is held to the document rules: whether it may change
// the type, what to do next when it breaks a rule, and whether only "the
// frontmatter parses" applies.
type opRules struct {
	keepType   bool   // the op is a small change, so changing the type is a mistake
	parseOnly  bool   // the op changes a template
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

// check holds after, an op's result, to the document rules against before,
// the file as the earlier ops leave it (nil for a new file). A finding the
// result shares with before is a warning; any other is the op's doing, and
// an error. It returns the result as a document, empty for a file without
// frontmatter.
func (p *batchPlan) check(r opRules, before, after []byte) (*document.Doc, []string, error) {
	voc, vocErr := p.vocabulary()
	was, wasErr := p.parse(before, voc)
	now, nowErr := p.parse(after, voc)
	switch {
	case nowErr != nil && wasErr != nil:
		return nil, []string{reason(nowErr) + "; fix the frontmatter with edit_text"}, nil
	case nowErr != nil:
		return nil, nil, fmt.Errorf("this write breaks the frontmatter: %s; %s", reason(nowErr), r.fixParse)
	case wasErr != nil:
		// Nothing to compare against: check the result as if the file had been
		// so all along, so what the rules find is a warning and a write can
		// repair it.
		was = now
	}
	if r.parseOnly {
		return now, nil, nil
	}
	var errs []error
	switch {
	case now.Type != was.Type && r.keepType:
		errs = append(errs, fmt.Errorf("this edit changes the type %q to %q; "+
			"keep the frontmatter's type line as it is", was.Type, now.Type))
	case now.Type == "" && was.Type != "":
		errs = append(errs, fmt.Errorf("this write removes the type %q, but every document needs one: "+
			"keep it, or set it to one of the configured types: %s", was.Type, p.w.cfg.TypeList()))
	}

	var broke, already []document.Finding
	for _, f := range now.Findings {
		switch {
		case f.Rule == document.RuleType && len(errs) > 0:
			// The type error says what to do about the type.
		case slices.ContainsFunc(was.Findings, f.Same):
			already = append(already, f)
		default:
			broke = append(broke, f)
		}
	}
	for _, msg := range p.messages(broke, now.Type, true, r.fixMissing) {
		errs = append(errs, errors.New(msg))
	}
	warnings := p.messages(already, now.Type, false, "give them values with update_frontmatter")
	// A plain file has no rules, so the tags file has nothing to vouch for.
	if vocErr != nil && now.Type != "" && len(now.Tags) > 0 {
		if slices.ContainsFunc(now.Tags, func(t string) bool { return !slices.Contains(was.Tags, t) }) {
			errs = append(errs, vocErr)
		} else {
			// It cannot vouch for an added tag, but it does not block a write that adds none.
			warnings = append(warnings, vocErr.Error())
		}
	}
	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	return now, warnings, nil
}

// parse reads src as a document, an empty one for a file without frontmatter.
func (p *batchPlan) parse(src []byte, voc *vault.Vocabulary) (*document.Doc, error) {
	d, err := document.Parse(src, p.w.cfg, voc)
	if d == nil && err == nil {
		return &document.Doc{}, nil
	}
	return d, err
}

// reason is why frontmatter does not parse, alone: the parser's advice to fix
// the file does not fit an op that broke it, nor a write that already succeeded.
func reason(err error) string {
	var fe *vault.FrontmatterError
	if errors.As(err, &fe) {
		return fe.Reason
	}
	return err.Error()
}

// messages words findings of a document of type typ, which the op broke or
// the file already broke: one message each, but one for every missing
// required field, ending in fixMissing.
func (p *batchPlan) messages(fs []document.Finding, typ string, broke bool, fixMissing string) []string {
	var out, missing []string
	flush := func() {
		if len(missing) > 0 {
			out = append(out, fmt.Sprintf("missing required fields for %s: %s; %s",
				typ, strings.Join(missing, ", "), fixMissing))
			missing = nil
		}
	}
	for _, f := range fs {
		if f.Rule == document.RuleRequired {
			missing = append(missing, f.Key.Field)
			continue
		}
		flush()
		out = append(out, f.Reason+p.nextStep(f.Rule, broke))
	}
	flush()
	return out
}

// nextStep is what to do after a finding of rule, which the op broke or the
// file already broke.
func (p *batchPlan) nextStep(rule document.Rule, broke bool) string {
	tagsFile := p.w.cfg.TagsFile
	switch {
	case rule == document.RuleType && broke:
		return "; " + p.useTypes()
	case rule == document.RuleType:
		return "; " + p.useTypes() + "; set it with update_frontmatter"
	case rule == document.RuleTag && broke:
		return ". Use a known tag (list_tags shows them all), or add it to " + tagsFile + " first"
	case rule == document.RuleTag:
		return ". Replace it with a known tag, or add it to " + tagsFile
	case broke:
		return ""
	}
	return "; set it with update_frontmatter"
}

// useTypes is what to do about a type the config does not define.
func (p *batchPlan) useTypes() string {
	return "use one of the configured types: " + p.w.cfg.TypeList()
}

// vocabulary reads the tags file as the earlier ops leave it, so a batch can
// add a tag and then use it.
func (p *batchPlan) vocabulary() (*vault.Vocabulary, error) {
	if f := p.byPath[path.Clean(p.w.cfg.TagsFile)]; f != nil && f.exists {
		return vault.ParseVocabulary(f.cur), nil
	}
	return p.w.v.Vocabulary(p.w.cfg.TagsFile)
}
