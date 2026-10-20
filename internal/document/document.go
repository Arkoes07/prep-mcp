// Package document reads a file as a document (GLOSSARY.md): its type,
// title, tags and date, and the document rules it breaks. read and write
// both see documents through it, so they agree on what a document says.
package document

import (
	"fmt"
	"strings"
	"time"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

// Rule is a document rule a Finding is about. "The frontmatter parses" is
// not one: Parse fails instead.
type Rule string

const (
	RuleType     Rule = "type"     // the type is configured
	RuleRequired Rule = "required" // the type's required fields have values
	RuleDate     Rule = "date"     // the date field holds a date
	RuleTag      Rule = "tag"      // the tags are in the vocabulary
)

// Key is what a Finding is about, as far as its rule tells findings apart:
// the type for RuleType; the type and field for RuleRequired; the type,
// field and value for RuleDate; and the tag, as Value, for RuleTag. So a
// changed type makes its required and date findings new, and an old bad tag
// stays the same finding.
type Key struct{ Type, Field, Value string }

// Finding is a document rule a document breaks. Reason says what is wrong;
// the caller adds what to do next.
type Finding struct {
	Rule   Rule
	Key    Key
	Reason string
}

// Same reports whether f and o are the same finding: the same rule, broken
// about the same key, whatever the reason says.
func (f Finding) Same(o Finding) bool { return f.Rule == o.Rule && f.Key == o.Key }

// Doc is a file with frontmatter, read as a document.
type Doc struct {
	Frontmatter *vault.Frontmatter
	Type        string // "" for a plain file, which no rule applies to
	Title       string // the title field
	Tags        []string
	DateField   string    // the type's date field, or date for a type not configured
	Date        time.Time // zero when DateField has no value or holds no date
	Findings    []Finding // in rule order: type, required, date, tag
}

// Parse reads src as a document. It returns nil, nil for a file without
// frontmatter, and the *vault.FrontmatterError for frontmatter that does not
// parse. A nil vocabulary skips the tag rule.
func Parse(src []byte, cfg *config.Config, voc *vault.Vocabulary) (*Doc, error) {
	fm, err := vault.SplitFrontmatter(src)
	if fm == nil || err != nil {
		return nil, err
	}
	return Of(fm, cfg, voc), nil
}

// Of reads parsed frontmatter as a document. A nil vocabulary skips the tag
// rule.
func Of(fm *vault.Frontmatter, cfg *config.Config, voc *vault.Vocabulary) *Doc {
	m := fm.Map
	d := &Doc{
		Frontmatter: fm,
		Type:        vault.Text(m, vault.FieldType),
		Title:       vault.Text(m, vault.FieldTitle),
		Tags:        vault.Values(m, vault.FieldTags),
	}
	d.DateField = cfg.DateField(d.Type)
	day, dateErr := vault.Day(m, d.DateField)
	d.Date = day
	if d.Type == "" {
		return d
	}

	spec, known := cfg.Types[d.Type]
	if !known {
		d.Findings = append(d.Findings, Finding{RuleType, Key{Type: d.Type}, fmt.Sprintf("unknown type %q", d.Type)})
	}
	for _, f := range spec.Required {
		if !vault.HasValue(m, f) {
			d.Findings = append(d.Findings, Finding{
				RuleRequired,
				Key{Type: d.Type, Field: f},
				fmt.Sprintf("missing required field for %s: %s", d.Type, f),
			})
		}
	}
	if dateErr != nil {
		// One string keeps Key comparable. Values practically never hold a NUL,
		// and two lists that join the same only make an error a warning.
		value := strings.Join(vault.Values(m, d.DateField), "\x00")
		d.Findings = append(d.Findings, Finding{RuleDate, Key{d.Type, d.DateField, value}, dateErr.Error()})
	}
	if len(voc.Tags()) > 0 {
		for _, t := range d.Tags {
			if !voc.Known(t) {
				d.Findings = append(d.Findings, Finding{
					RuleTag,
					Key{Value: t},
					fmt.Sprintf("unknown tag %q; closest known tag: %q", t, voc.Closest(t)),
				})
			}
		}
	}
	return d
}
