---
id: 3
title: Add test coverage for collections and project resolver
status: doing
priority: medium
tags:
  - testing
  - phase-0
---

# Add Test Coverage for Collections and Project Resolver

Add table-driven tests for critical untested paths in the collections and project resolver systems.

## Priority Areas

- `internal/project/collections.go` — collection loading, flattening, clip building
- `internal/project/resolver.go` — whatever remains after legacy cleanup (PH-1)
- `internal/cli/index_filter.go` — index range parsing

## Acceptance Criteria

- Test coverage exists for collections resolver
- Test coverage exists for project resolver
- Index filter parsing has table-driven tests
- All tests pass
