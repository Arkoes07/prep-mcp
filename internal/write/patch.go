package write

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

type Patch struct {
	Set                        *yaml.Node // ordered mapping of keys to set; nil for none
	Unset, AddTags, RemoveTags []string
}

// NewPatchOp returns the op that changes frontmatter keys and tags, leaving the
// body's bytes alone.
func NewPatchOp(path string, patch Patch) Op {
	return Op{tool: ToolPatch, target: path, apply: func(p *batchPlan) (*file, []byte, []string, error) {
		return p.modify(path, patchRules, func(f *file, before parsed) ([]byte, *vault.Frontmatter, []string, error) {
			// No updated date on a template: every document made from it would copy it.
			return applyPatch(before, patch, p.w.now().Format(time.DateOnly), !f.template)
		})
	}}
}

// applyPatch changes the keys p names in the frontmatter before and returns
// the new file, its frontmatter and the patch's warnings. A patch that changes
// nothing returns the file as it was. With stamp, any change also sets updated
// to today, unless the patch sets or unsets it.
func applyPatch(before parsed, p Patch, today string, stamp bool) ([]byte, *vault.Frontmatter, []string, error) {
	fm, err := before.fm, before.err
	if err != nil {
		return nil, nil, nil, err
	}
	if fm == nil {
		return nil, nil, nil, errors.New("no frontmatter to patch; use replace_doc to give this file frontmatter")
	}
	setKeys, err := checkPatch(p)
	if err != nil {
		return nil, nil, nil, err
	}

	ed := fm.Edit()
	for _, k := range p.Unset {
		ed.Unset(k)
	}
	for key, val := range vault.Pairs(p.Set) {
		if key.Value != vault.FieldTags {
			ed.Set(key.Value, val)
		}
	}
	var warnings []string
	if slices.Contains(setKeys, vault.FieldTags) {
		ed.SetTags(vault.Values(p.Set, vault.FieldTags))
	} else if p.hasTagOps() {
		var tags []string
		tags, warnings = applyTagOps(vault.Values(fm.Map, vault.FieldTags), p)
		ed.SetTags(tags)
	}
	// setKeys, not what changed: a set to the value updated already has keeps it.
	ownUpdated := slices.Contains(setKeys, vault.FieldUpdated) || slices.Contains(p.Unset, vault.FieldUpdated)
	if stamp && ed.Changed() && !ownUpdated {
		ed.Set(vault.FieldUpdated, vault.NewStrNode(today))
	}
	out, after, err := ed.Apply()
	var fe *vault.FrontmatterError
	switch {
	case errors.As(err, &fe):
		return nil, nil, nil, err // the file's layout, which its Fix says how to change
	case err != nil:
		return nil, nil, nil, fmt.Errorf(
			"internal error: %w; nothing was written. Report this as a bug and use replace_doc instead",
			err,
		)
	}
	return out, after, warnings, nil
}

// checkPatch rejects a patch that contradicts itself and returns the keys of
// p.Set in order.
func checkPatch(p Patch) ([]string, error) {
	setKeys, err := listSetKeys(p.Set)
	if err != nil {
		return nil, err
	}
	for _, k := range p.Unset {
		switch {
		case slices.Contains(setKeys, k):
			return nil, fmt.Errorf("%s is in both set and unset; choose one", k)
		case k == vault.FieldTags && p.hasTagOps():
			return nil, errors.New("tags is unset and also changed by add_tags/remove_tags; use one or the other")
		}
	}
	if slices.Contains(setKeys, vault.FieldTags) && p.hasTagOps() {
		return nil, errors.New("set.tags cannot be combined with add_tags or remove_tags; use one or the other")
	}
	return setKeys, nil
}

func (p Patch) hasTagOps() bool { return len(p.AddTags) > 0 || len(p.RemoveTags) > 0 }

// applyTagOps returns the tag list add_tags and remove_tags make of cur, with a
// warning for each tag to remove that is not there.
func applyTagOps(cur []string, p Patch) ([]string, []string) {
	want := slices.Clone(cur)
	for _, t := range vault.NormalizeTagList(p.AddTags) {
		if !slices.Contains(want, t) {
			want = append(want, t)
		}
	}
	var warnings []string
	drop := vault.NormalizeTagList(p.RemoveTags)
	for _, t := range drop {
		if !slices.Contains(cur, t) {
			warnings = append(
				warnings,
				fmt.Sprintf("tag %q is not on this document, so there was nothing to remove", t),
			)
		}
	}
	return slices.DeleteFunc(want, func(t string) bool { return slices.Contains(drop, t) }), warnings
}

func listSetKeys(set *yaml.Node) ([]string, error) {
	if set == nil {
		return nil, nil
	}
	if set.Kind != yaml.MappingNode {
		return nil, errors.New("set must be an object of key: value pairs")
	}
	keys := make([]string, 0, len(set.Content)/2)
	for k := range vault.Pairs(set) {
		keys = append(keys, k.Value)
	}
	return keys, nil
}
