# Implementation Plan — Live Review Notes

## Protocol

Lead tech writes in `specs/debug-db/plan/`, reviewer writes to THIS FILE only.
Do NOT modify each other's files. Respond constructively.

---

### [2026-05-13 13:55] Lead tech: Plan written

Four files created in `specs/debug-db/plan/`:

1. **README.md** — Overview + dependency graph revised for maximum
   parallelism. Two-track execution: backend (Dev A) and TUI (Dev B).
   Key insight: TUI can be built with mock data before any backend exists,
   delivering an early demo milestone. 3-4 calendar days with two
   developers (vs 5-8 sequential).

2. **phase-00.md** — Backend split into 5 sub-tasks (00.1–00.5).
   00.1 (SQL policy) and 00.2 (value formatter) can run in parallel.
   Concrete insertion point for `/dqlite` route: after line 1045 in
   `apiserver/apiserver.go`. `dqliteDBGetter` field on
   `sharedServerConfig`.

3. **phase-01.md** — Client package with 3 sub-tasks. Flags a spec issue
   (§R1): `OpenDqlite` declared with `base.StreamConnector` but calls
   `ConnectControllerStream`. `/dqlite` is controller-scoped
   (`noModelUUID: true`), so the wider interface is needed. Plan documents
   this and uses `ConnectControllerStream` with a note that
   `api.Connection` satisfies it.

4. **phase-02.md** — TUI split into 8 sub-tasks (02.1–02.7 with 02.1b).
   Starts with local types to enable parallel development before 01.1
   types land. 02.1b aligns to `api/common`. Early demo milestone at
   completion of 02.5 (mock-based TUI fully functional with sample data).
   Concrete registration location: after line 373 in
   `cmd/juju/commands/main.go`.

---

### [2026-05-13 14:30] Reviewer: Agent-execution improvement notes

**Context**: The hackathon will delegate all implementation to coding
agents. The current plan is written for human developers. Below are the
refinements needed before launching agents.

---

#### 1. Task Granularity: Split into Single-File Agent Prompts

Each sub-task must be extractable into a standalone agent prompt with
one-file / one-func scope.

**What to change**:
- Split `phase-00.md` into `plan/tasks/00.1-sql-policy.md`,
  `plan/tasks/00.2-value-formatter.md`, `plan/tasks/00.3-handler-core.md`,
  `plan/tasks/00.4-route-wiring.md`, `plan/tasks/00.5-tests.md`.
- Same for phase-01 (01.1–01.3) and phase-02 (02.1–02.7 + 02.1b).
- Each task file must contain:
  - **Exact files to create/modify** (1–3 files max per task).
  - **Files to read first** with line ranges (agents must Read before
    Edit).
  - **Complete test command** to run after.
  - **Type/stub/function signatures** in ready-to-copy Go syntax.
  - **Verification**: `go build ./pkg/... && go test -race ./pkg/...`.
  - **Memory update**: path to `specs/debug-db/memory/phase-XX.md` to
    write after completion.
  - **Handoff**: "After this task, update `impl-plan-agent.md` with
    completion status and any deviations found."

**Why**: Agents need self-contained, atomic prompts. Multi-file
instructions cause scope drift and missed verification steps.

---

#### 2. Explicit AGENTS.md Injection Per Task

Every task must start with a mandatory rules preamble.

**What to add** — prepend to each task file in `plan/tasks/`:

```
## Agent Rules (apply before any code change)

- Read and apply `AGENTS.architecture-rules.md`:
  - `apiserver/` must not depend on `cmd/`.
  - No new cross-layer dependencies.
  - No goroutines inside API handlers.
- Read and apply `AGENTS.core-domain-rules.md` for lifecycle/relation
  constraints (if touching domain code).
- Read and apply `AGENTS.md` test conventions:
  - Use `tc` for test helpers.
  - `c.Assert(err, tc.ErrorIsNil)` / `c.Check(val, tc.Equals, expected)`.
  - For `select`, use `c.Context` instead of timeouts.
- Read `AGENTS.documentation.rules.md` if writing doc.go or user-facing docs.
- Read `AGENTS.doc-dot-go-rules.md` if creating a new package.
```

**Why**: Agents don't auto-discover AGENTS.md content. Rules must be
injected inline at the top of each task prompt. This prevents silent
violations of layering, test conventions, or comment rules.

---

#### 3. Verification Gates After Every Task

The plan lists acceptance criteria per phase, not per sub-task. Agents
need a gate after each subtask to catch regressions early.

**What to add** — in every task file, a `## Verification` section:

```
## Verification

Run the following in order. If any fails, fix before proceeding:

1. `go build ./pkg/...`  (replace ./pkg/ with affected package)
2. `go test -race -count=1 ./pkg/...`  (all tests in affected package)
3. `make pre-check` if the task touches >1 package or adds new files
```

For tasks touching apiserver handler code (goroutines, websocket):
```
4. `go test -race -count=5 ./apiserver/...`  (stress for race conditions)
```

**Why**: A single agent executing 00.3 (handler) without building can
silently break the import tree. Gates prevent cascading failures that
only surface when another agent picks up 00.4 (wiring).

---

#### 4. Parallel Agent Launch Plan

The README already defines Track A (backend) and Track B (TUI). Agents
can multiply this — launch up to 4 agents concurrently when dependencies
allow.

**What to add** — a `plan/tasks/CONCURRENCY.md` listing which task
combinations can run in the same batch, with explicit "must NOT overlap"
constraints.

Example batch:
```
Agent 1 (task 00.1): SQL policy validator   → dqlite_policy.go
Agent 2 (task 00.2): Value formatter        → dqlite_format.go
Agent 3 (task 02.1): DqliteAPI + local types → cmd/juju/debug/dqlite_api.go
Agent 4 (task 02.2): dqliteModel struct     → cmd/juju/debug/dqlite_model.go
```

**Critical constraint**: Two agents must never edit the same file
concurrently. The task files must name disjoint file sets. Current plan
already achieves this (00.1→dqlite_policy.go, 00.2→dqlite_format.go,
00.3→dqlite.go, 00.4→apiserver.go/shared.go, 00.5→*_test.go files).

---

#### 5. Spec-Sync Diff Step

The plan mentions deviations from the original spec in a table, but
agents won't diff unless told to.

**What to add** — in each task file:

```
## Spec Alignment

Before starting, diff against the original spec:
- `specs/debug-db/phase-00-websocket-backend.md` (for phase 00 tasks)
- `specs/debug-db/phase-01-client-package.md` (for phase 01 tasks)
- `specs/debug-db/phase-02-tui.md` (for phase 02 tasks)

If the plan deviates from the spec, the task MUST include the deviation
in its comments/commit message. Flag any AGENTS.md rule violations that
the plan introduces (e.g., apiserver importing cmd/).
```

**Why**: The plan already documents deviations (D1-D3 in phase-00.md,
R1 in phase-01.md, D1-D3 in phase-02.md). Agents must carry these
forward into code. Without explicit instruction, an agent implementing
phase-01 will use `ConnectStream` (as spec says) instead of
`ConnectControllerStream` (as the plan corrects in §R1).

---

#### 6. Memory Updates as Agent Handoff

The plan says "Write `specs/debug-db/memory/phase-XX.md`" but doesn't
specify what an agent should record. Without structure, different agents
write incompatible memory formats.

**What to standardize** — template in each task file:

```
## Post-Completion Memory

After the task passes verification, update
`specs/debug-db/memory/phase-XX.md` with:

- [ ] Exact file paths created/modified (with line ranges).
- [ ] Any import additions (package + reason).
- [ ] Test coverage: list of test function names added.
- [ ] Deviations from plan discovered during implementation.
- [ ] Dependencies introduced (new Go module imports, new interfaces).
- [ ] Known limitations or TODOs left for future phases.
```

**Why**: `phase-01.md` needs to know exact JSON field names and response
shapes from `phase-00.md`. Ad-hoc memory = missed details = broken
handoffs.

---

#### 7. Mock Isolation in Track B

Tasks 02.1–02.5 must be fully mock-driven. The plan achieves this with
local types (02.1) and `mockDqliteAPI` (02.7). This is correct but needs
one additional guard.

**What to add** — in `plan/tasks/02.2-dqlite-model.md` through
`plan/tasks/02.5-view-rendering.md`:

```
## Isolation Rule

This task MUST NOT import `api/common`, `apiserver/`, or any Dqlite
database package. The only dependency allowed is the local `DqliteAPI`
interface defined in 02.1.

If the task needs real types, stop and wait for 01.1 + 02.1b.
```

**Why**: A well-meaning agent might import `common.DqliteClient` to make
the mock more realistic, breaking Track B's independence from Track A
and delaying the early demo milestone.

---

#### 8. Bubbletea Dependency Verification

Phase 02 introduces bubbletea/bubbles/lipgloss. These may not be in
`go.mod` / `go.sum`. The plan doesn't mention this.

**What to add** — in `plan/tasks/02.2-dqlite-model.md`:

```
## Dependency Check

Before writing any bubbletea code, verify these are in go.mod:
- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`
- `github.com/charmbracelet/lipgloss`

If missing, run: `go get github.com/charmbracelet/bubbletea`
```

**Why**: An agent that writes 500 lines of bubbletea code before
discovering the dependency is missing causes wasted context and re-tries.

---

#### 9. Summary: Files the Plan Should Produce

For the lead tech to create before the hackathon:

```
specs/debug-db/plan/
├── README.md                         ← keep, add agent preamble
├── CONCURRENCY.md                    ← NEW: parallel launch matrix
├── phase-00.md                       ← keep as reference
├── phase-01.md                       ← keep as reference
├── phase-02.md                       ← keep as reference
└── tasks/
    ├── AGENT_RULES.md                ← NEW: shared rules preamble
    ├── 00.1-sql-policy.md
    ├── 00.2-value-formatter.md
    ├── 00.3-handler-core.md
    ├── 00.4-route-wiring.md
    ├── 00.5-backend-tests.md
    ├── 01.1-exported-types.md
    ├── 01.2-client-methods.md
    ├── 01.3-client-tests.md
    ├── 02.1-dqlite-api-local-types.md
    ├── 02.1b-align-api-common.md
    ├── 02.2-dqlite-model.md
    ├── 02.3-load-messages.md
    ├── 02.4-keyboard-update.md
    ├── 02.5-view-rendering.md
    ├── 02.6-command-registration.md
    └── 02.7-tui-tests.md
```

---

#### 10. Risk Additions for Agent Execution

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Agent writes code without reading AGENTS.md | High | High | Inline rules preamble in every task (item 2) |
| Two agents edit same file concurrently | Medium | High | CONCURRENCY.md with explicit file ownership (item 4) |
| Agent skips `make pre-check` / `go build` | High | Medium | Mandatory verification section per task (item 3) |
| Agent uses `ConnectStream` instead of `ConnectControllerStream` | Medium | Medium | Spec-sync diff step + §R1 note in task (item 5) |
| Agent adds real import in mock task | Medium | Medium | Isolation rule with banned import list (item 7) |
| Agent can't find bubbletea dependency | High | Low | Pre-flight dependency check (item 8) |
| Memory files diverge in format | Medium | Medium | Standardized memory template (item 6) |
| Agent overwrites existing apiserver.go incorrectly | Low | High | Exact line ranges and oldString blocks in task files |

---

### Reviewer Sign-off

The plan as written is correct and complete for human developers. For
agent execution during the hackathon, the refinements above (items 1–10)
are necessary. Estimated effort to produce the task files: **1–2 hours**
for the lead tech.

Priority order for refinement:
1. Split into task files (item 1) + rules preamble (item 2) — **blocking**.
2. Verification gates (item 3) + spec-sync step (item 5) — **blocking**.
3. Concurrency matrix (item 4) + mock isolation rules (item 7) — **high**.
4. Memory template (item 6) + dependency check (item 8) — **medium**.

The plan's architecture decisions, wire protocol, type definitions, and
interface boundaries are sound and need no changes. This review only
addresses executability by agents.
