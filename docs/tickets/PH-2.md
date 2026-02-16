---
id: 2
title: Fold config validation into check --strict
status: doing
priority: medium
tags:
  - validation
  - phase-0
---

# Fold Config Validation into check --strict

Extend `powerhour check --strict` to validate configuration references and catch common misconfigurations early.

## Validations to Add

- Profile names referenced by collections exist in `profiles.overlays`
- Collection plan file paths exist on disk
- Segment template tokens are valid
- Orphaned profile definitions (warning, not error)

## Acceptance Criteria

- `powerhour check --strict` validates config references
- Clear error messages for each validation failure
- Orphaned profiles produce warnings, not errors
