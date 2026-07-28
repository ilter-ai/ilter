## Summary

<!-- One sentence: what does this PR do? -->

## Why

<!-- Context: what problem does this solve, or what issue does it fix? Link to issue if applicable. Closes #XXX -->

## Changes

<!-- List the key changes. Be specific enough that a reviewer knows where to look. -->

- 
- 

## Checklist

- [ ] `make check` passes (Go build + lint + web build + web lint)
- [ ] `make test` passes (unit + integration)  
- [ ] New/changed behavior has tests
- [ ] If middleware chain order changed → updated `internal/middleware/doc.go`
- [ ] If new external dependency added → justified in this PR description
- [ ] If DB schema changed → new migration file added in `internal/db/migrations/`
- [ ] If architecture decision changed → linked or updated relevant ADR in `docs/decisions/`

## Testing Notes

<!-- How did you test this? What edge cases did you consider? -->

## Screenshots (if UI change)

<!-- Dashboard screenshots before/after, if applicable -->
