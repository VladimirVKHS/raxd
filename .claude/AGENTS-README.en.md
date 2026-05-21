# raxd Subagent Team — How to Use

A set of **24 subagents** for building `raxd`: 12 functional roles + 12 guardians (one per role).
The conductor is the main Claude Code at the repo root; its rules live in `CLAUDE.md`.

## Roster and order (pipeline)

```
pm → research-analyst → architect → security → (cli-ux ‖ mcp-engineer ‖ system-dev)
   → developer → (devops ‖ qa) → reviewer → tech-writer
```

After each step its **guardian** verifies the output (read-only) and returns a verdict
`pass | needs-changes | blocked`. `needs-changes`/`blocked` → back to the role.

| Role | Does | Artifact |
|---|---|---|
| `pm` | requirements, acceptance criteria, scope | `specs/<id>/spec.md` |
| `research-analyst` | external research (Go/MCP/security) | `specs/<id>/research.md`, ADR |
| `architect` | one chosen approach, modules, contracts | `specs/<id>/plan.md` |
| `security` | threat model, security requirements | `threat-model.md`, `security-requirements.md` |
| `cli-ux` | console output design, banner, tables | `specs/<id>/ux-spec.md` |
| `mcp-engineer` | MCP design (tools/resources/transport) | `specs/<id>/mcp-spec.md` |
| `system-dev` | service (systemd/launchd), cross-build | service files, `service-design.md` |
| `developer` | code per plan/spec | branch per git-flow |
| `devops` | install.sh, goreleaser, CI | `install.sh`, `.goreleaser.yaml` |
| `qa` | test plan and tests | `test-plan.md` + tests |
| `reviewer` | code review vs spec+plan (read-only) | `specs/<id>/review.md` |
| `tech-writer` | product documentation | `docs/**` |

## How to invoke

- **Automatically**: describe the task in plain language; the main Claude routes by the
  `description` field. Example: "describe requirements for key export" → `pm`.
- **Explicitly**: `@architect design the architecture` — guarantees a specific role.

## Access tiers (tools scoping)

- **Authors** (pm, research-analyst, architect, security, cli-ux, mcp-engineer, tech-writer):
  `Read, Grep, Glob, Write, WebFetch, Skill` — write only md artifacts, never touch code
  (no `Edit`/`Bash`).
- **Builders** (system-dev, developer, devops, qa): `+ Edit, Bash` — write code and run commands.
- **Verifiers** (reviewer + all guardians): `Read, Grep, Glob` — read-only. They return their
  report as text; the **main Claude persists** it to the artifact file. This is the architectural
  independence guarantee: without `Write/Edit/Bash` they physically cannot change code.

## Reference (shared knowledge)

`.claude/reference/` is the single source of truth agents read before working:
`STACK.*` (stack), `SECURITY-BASELINE.*` (mandatory security checklist),
`MCP-INTEGRATION.*` (transport/SDK). Change the stack here, not in every agent.

## Tuning

- **Model**: edit `model:` in frontmatter (`opus` for reasoning, `sonnet` for routine,
  `haiku` for cheap guardian checks).
- **Skills**: listed in each agent's "Skills" section (invoked via the `Skill` tool).
- **Product name** `raxd`: change it in `.claude/reference/STACK.*` and `CLAUDE.md`.

## Verify the install

Run `claude` at the root of `test_project/` and type `/agents` — all 24 names should appear.

**If guardians are missing** (some Claude Code builds don't scan 2-level nesting): move guardian
files from `.claude/agents/guardians/<name>/<name>.md` to a flat `.claude/agents/<name>.md`,
keeping their docs/templates in the subfolder. Agent identity is the `name` field, not the path,
so nothing else needs to change.

Author: **Vladimir Kovalev, OEM TECH**.
