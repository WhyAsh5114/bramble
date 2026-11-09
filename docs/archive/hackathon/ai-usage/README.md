# Agent usage fragments

Each agent records the paths it wrote in `docs/ai-usage/<agent-name>.md` (for example `core-claude.md`), one bullet per change, appended and never rewritten. This avoids merge conflicts in `docs/AI_USAGE.md`.

Run `node scripts/combine-ai-usage.mjs` to copy every fragment into `docs/AI_USAGE.md`, between the `fragments:start` and `fragments:end` markers. Atlas runs it before the final README review.
