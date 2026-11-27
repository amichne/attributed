---
name: git-attributed
description: Install, configure, inspect, or troubleshoot local human and AI line attribution.
license: MIT
compatibility:
  - factory-ai
  - claude-code
allowed-tools:
  - Read
  - Execute
  - Grep
---

# git-attributed

Use `/git-attributed-setup` to activate the agent and Git hooks.

Use:

```sh
git-attributed status
git-attributed blame path/to/file
git-attributed blame --json path/to/file
git-attributed verify
git notes --ref=attributed show HEAD
```

Attribution data stays in local Git objects, worktree state, and
`refs/notes/attributed`. Git hook installation shares the notes ref on ordinary
pushes unless `--local-notes` was selected. Never expose checkpoint content,
repository paths, session identifiers, or note data in public logs. Never push
retention refs.
