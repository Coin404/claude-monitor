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
CLAUDE.md                        — dev reference, hook format, state machine
README.md                        — user-facing docs, features, setup
main.go                          — systray UI, detection loop, menu items
internal/detect/hooks.go         — hook writing to settings.json/settings.local.json
internal/detect/process.go       — Claude process listing, blocked PID detection
internal/detect/state.go         — per-session state file reading
internal/detect/terminal.go      — terminal app detection, window activation
internal/detect/activate_darwin.go — native NSRunningApplication activation (cgo)
internal/detect/sessions_bridge.go — session data bridge for SwiftUI panel
internal/core/status.go          — status enum, color constants, display names
internal/icon/icon.go            — tray icon generation
internal/stats/tracker.go        — daily status duration tracking, HTML chart
helpers/stats_window.swift       — native stats chart WKWebView window
helpers/sessions_panel.swift     — native SwiftUI glass-style sessions panel
```

## Output format
After reviewing, give a concise summary:
- Which docs were updated and why
- Which docs were left alone and why
- Any gaps or follow-ups the user should address
