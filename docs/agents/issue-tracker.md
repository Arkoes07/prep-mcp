# Issue tracker: GitHub Issues

Issues and specs for this repo live in GitHub Issues on `Arkoes07/prep-mcp`. Use the `gh` CLI for everything.

## Conventions

- One issue per ticket, never a single combined tickets issue.
- Group a feature's issues with a `feature:<feature-slug>` label. Create the label with `gh label create` if it is new.
- A feature's spec is an issue titled `Spec: <feature>` with the same `feature:` label.
- Triage state is a label (see `triage-labels.md` for the role strings). Swap labels with `gh issue edit <n> --add-label … --remove-label …`.
- Done work closes the issue: `gh issue close <n> --comment "Done in <sha>"`. There is no done label.
- Comments and conversation go in issue comments: `gh issue comment <n>`.

## When a skill says "publish to the issue tracker"

`gh issue create --title … --body … --label needs-triage --label feature:<feature-slug>`

## When a skill says "fetch the relevant ticket"

`gh issue view <n> --comments`. The user will normally pass the issue number.

## Wayfinding operations

Used by `/wayfinder`. The **map** is an issue with one **child** issue per ticket.

- **Map**: an issue titled `Map: <effort>` with the `map` label, holding the Notes / Decisions-so-far / Fog body.
- **Child ticket**: an issue with the question in the body and a `Part of #<map>` line at the top. A `type:<research|prototype|grilling|task>` label records the ticket type.
- **Blocking**: a `Blocked by: #N, #N` line near the top. A ticket is unblocked when every issue it lists is closed.
- **Frontier**: `gh issue list --search "\"Part of #<map>\" in:body" --state open` and keep the unblocked, unassigned ones; lowest number wins.
- **Claim**: `gh issue edit <n> --add-assignee @me` before any work.
- **Resolve**: comment the answer under an `## Answer` heading, close the issue, then edit the map's Decisions-so-far to add a context pointer (gist + `#<n>`).
