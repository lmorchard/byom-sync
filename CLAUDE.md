# CLAUDE.md — byom-sync

## Always use PRs

**Never push directly to `main`.** All work goes: branch → PR → CI green → merge.
This holds for docs and one-line fixes too, not just features.

Do code work in a worktree under `./.claude/worktrees/` rather than the shared
primary checkout — several agents may be working in this repo at once, so run
`git worktree list` first and branch off `origin/main`.

## Everything else

Full context for coding agents — stack, layout, commands, conventions, and
gotchas — lives in AGENTS.md:

@AGENTS.md
