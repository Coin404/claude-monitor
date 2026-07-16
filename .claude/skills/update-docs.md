---
name: update-docs
description: After completing a feature, review and update all project documentation (CLAUDE.md, README.md, TODO.md).
---

# Update Project Documentation

You are helping maintain the **claude-monitor (Novascope)** project. After every feature or significant change, follow this checklist to keep docs in sync.

## Steps

### 1. Review what changed
Look at the git diff to understand what files were added, modified, or removed:
```bash
git diff --stat HEAD~1
```

### 2. Update CLAUDE.md
`CLAUDE.md` is the primary development reference. Check each section:

- **Project overview** — still accurate? New dependencies?
- **Hook format** — any hook changes?
- **Color state machine** — new states or changed mappings?
- **Key files** — new files added? Responsibilities shifted?
- **Timing / Git workflow** — any changes?

### 3. Update README.md
- Feature list still current?
- Setup / build instructions still work?
- New features documented?

### 4. Update TODO.md
- Mark completed items as done
- Remove items that are no longer relevant
- Add follow-up items discovered during implementation

### 5. Check for stale comments or dead code
- Any `// TODO` or `// FIXME` related to the completed feature?
- Any unused functions or variables left behind?

## Key files to check
```
CLAUDE.md        — dev reference, hook format, state machine
README.md        — user-facing docs, features, setup
TODO.md          — task tracking
main.go          — systray UI, detection loop, menu items
detector.go      — hook writing, process/proxy detection
stats.go         — stats tracking, HTML report generation
stats_window.swift — native stats chart window
icon.go          — tray icon generation
activate_darwin.go — native NSRunningApplication activation
```

## Output format
After reviewing, give a concise summary:
- Which docs were updated and why
- Which docs were left alone and why
- Any gaps or follow-ups the user should address
