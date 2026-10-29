# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Assetto Server Manager is a Go web app (chi router, server-rendered Go templates, TypeScript frontend) that manages
an Assetto Corsa dedicated server (`acServer`): race configuration, championships, race weekends, live timing,
results and content upload. This is a fork of JustaPenguin/assetto-server-manager that is being developed further.

## Working Agreement

These rules override the conservative git defaults of the Beads block below.

- **Plan work in beads**: a feature is an epic, its concrete steps are tasks under it (`bd create ... --parent=<epic>`).
  Create/claim the bead before writing code. `todo.md` is the user's scratch list of ideas; turn items into beads
  instead of tracking progress there.
- **Commit after every finished feature/task**: tests green, bead closed, then commit. One logical change per
  commit, imperative English subject in the style of the history (e.g. "Add read-only championship API").
  **Do not push** (git or `bd dolt push`) unless asked.
- User-facing changes go into `CHANGELOG.md` (embedded and shown in the UI); API changes into the API section of
  `README.md`.

## Build & Test

`go.mod` targets Go 1.13 with pinned, old dependencies — don't bump them as a side effect.

The root package imports generated, gitignored embed files (`cmd/server-manager/{static,views}/static_embed.go`,
`internal/changelog/changelog_embed.go`), so run `go generate` on a fresh checkout and after changing
`CHANGELOG.md` or static files, otherwise nothing compiles:

```bash
go install github.com/mjibson/esc@v0.2.0   # once
go generate ./...

go test ./...                               # all tests
go test -run TestNewAPIChampionship ./internal/championship   # single test in its package       # single test (root package is `servermanager`)
go vet ./...
```

`make test` additionally copies `fixtures/results/*.json` into `cmd/server-manager/assetto/results` and runs with
`-race`; some results-based tests rely on that. The Makefiles are Unix-only (`rm`, `cp`, `sed`).

Run locally on Windows without a real AC server (fake install in `dev/assetto-seed`), login `admin`/`admin` at
http://localhost:8772:

```powershell
./dev/run.ps1             # generate assets, build, start (frontend built on first run only)
./dev/run.ps1 -Frontend   # also rebuild TypeScript/SCSS
./dev/run.ps1 -Reset      # wipe local DB/config/fake install
./dev/run.ps1 -NoRun      # build only
./dev/run.ps1 -Seed       # fill the DB with test data first (cmd/utils/dev-seed; server must be stopped; idempotent)
docker compose up --build # alternative: production Dockerfile with dev config
```

The server runs from `cmd/server-manager` with `FILESYSTEM_HTML=true`, so template changes only need a browser
refresh; Go changes need a restart. Actually starting a race doesn't work locally (no `acServer` binary).

Frontend: `cd cmd/server-manager/typescript && npm ci && npx gulp build` (`npx gulp` to watch); output goes to
`cmd/server-manager/static`. See `DEVELOPMENT.md` for more (e.g. proxy CA for Docker builds).

## Architecture

The root package `servermanager` is only the wiring layer (about 1.6k lines including tests); all domain code lives in packages under
`internal/` and `pkg/`:

- **Root (wiring only)**: `resolver.go` (manual dependency wiring, UDP dispatch `Resolver.UDPCallback`), `router.go`
  (routes and permission groups), `template_funcs.go` (template functions that need domain code), `init.go`
  (`InitWithResolver`: startup, signal handling, scheduled events), `servermanager_config.go` (`BuildStore`),
  `monitoring.go` (Prometheus, panic capture), `lua.go`. `cmd/server-manager/main.go` only wires config, store,
  migrations, UDP and HTTP together. The root stays the wiring package because `main.go`, `cmd/utils/*` and the
  `-X github.com/wernersbacher/assetto-server-manager.BuildVersion` ldflags (Makefile, Dockerfile, `dev/run.ps1`)
  reference it.
- **Dependency direction** (roughly bottom to top; a package imports only packages listed before it, no cycles):
  `pkg/*`, `acserver/serverconfig`, `plainname`, `formfields`, `lua`, `results`; `model`, `content`, `plugins`; `store`,
  `config`; `web` (renderer, `BaseHandler`, access checks, form encoding); `serverenv`; `server` (AC process, content
  manager wrapper); `accounts`, `api`, `notification`, `schedule`, `racecontrol`; `race`; `championship`;
  `raceweekend`; the handler packages `contenthttp`, `resultshttp`, `pluginhttp`, `admin`; the root.
- **Domain packages with their own HTTP handlers**: `race` (custom/quick races), `championship`, `raceweekend`,
  `racecontrol` (live timing, penalties), `schedule` (calendar, upcoming races), `accounts`. Each holds its `*Manager`
  (business logic) and `*Handler` (thin HTTP layer embedding `smweb.BaseHandler`).
- **Handler-only packages** for handlers that cannot live in the package of their data (those packages are imported by
  `web`, so they cannot import it): `contenthttp` (cars, tracks, weather, setups, uploads), `resultshttp` (results
  list/API/collisions/delete), `pluginhttp` (KissMyRank, Real Penalty, stracker), `admin` (server administration,
  healthcheck, event details; they need several managers).
- **Process state**: `serverenv.State` holds the install path, server id and the fallback-sorting option and builds the
  `serverenv.Env` which managers and handlers get injected (`env.InstallPath()`, `env.Results()`, ...). The values are
  functions on purpose: migrations run (in `BuildStore`) before the install path is set from the config, which `main` does before `NewResolver` because the car and track managers are given the path at creation. Remaining globals:
  `BuildVersion` (ldflags target), `Debug` (from `$DEBUG`), the log buffers / `panicCapture` and the Prometheus metrics
  in `router.go` / `monitoring.go` (process-wide singletons), `smresults.UseShortenedDriverNames`.
- **Where new code goes**: a new manager/handler goes into the domain package it belongs to (a handler-only package if
  it would otherwise form a cycle), gets exported handler methods, is created in a `resolveXxx()` method of
  `resolver.go` and is passed to `Router()`; do not read package globals, take a `*serverenv.Env`.
- **Routing & permissions** (`router.go`): routes are grouped by `AccountHandler` access-level middleware
  (read / write / delete / admin) — put new routes in the group matching the required permission. JSON APIs live
  under `/api/...`.
- **Persistence**: everything goes through the `Store` interface (`internal/store`) with two implementations,
  `BoltStore` and `JSONStore` — new persisted data must be implemented in both. Stored types live in `internal/model`
  (registered with `gob.RegisterName`; the names are the persisted format, never change them). Changes to stored
  structs that need data conversion get a migration appended to `migrations` (never reorder/remove entries; the stored
  version is the slice index).
- **AC server integration**: `server.AssettoServerProcess` (OS-specific files) starts/stops `acServer` and plugins
  after writing `server_cfg.ini` / `entry_list.ini`. Live data comes back via the AC UDP plugin protocol (`pkg/udp`)
  and is dispatched by `Resolver.UDPCallback` to race control, championship and race weekend managers.
- **Forms**: many edit forms are generated from struct tags via `github.com/cj123/formulate`
  (`web.EncodeFormData` / `web.DecodeFormData`).
- **Templates** live in `cmd/server-manager/views` (`layout/`, `pages/`, `partials/`), rendered by `web.Renderer`
  (sprig + `web` funcs + `serverTemplateFuncs`); views and static files are embedded with `esc`. Templates call
  methods and functions by name at runtime: after moving or changing a symbol, grep the views for it and smoke-test
  the pages (the compiler does not check templates).
- **Tests**: helpers live in `envtest` (`serverenv.Env` for a temp install), `webtest` (request scope for handlers),
  `server/servertest` (`Dummy`, `Interruptible` process), `notification/notificationtest`. Results/championship
  fixtures are read from `fixtures/` and `cmd/server-manager/assetto`.
- `pkg/` holds standalone libraries: `udp` (AC UDP protocol), `acd` (encrypted car data), `when` (timers).

## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/core-concepts/sync-concepts.md for details and anti-patterns.

## Agent Context Profiles

The managed Beads block is task-tracking guidance, not permission to override repository, user, or orchestrator instructions.

- **Conservative (default)**: Use `bd` for task tracking. Do not run git commits, git pushes, or Dolt remote sync unless explicitly asked. At handoff, report changed files, validation, and suggested next commands.
- **Minimal**: Keep tool instruction files as pointers to `bd prime`; use the same conservative git policy unless active instructions say otherwise.
- **Team-maintainer**: Only when the repository explicitly opts in, agents may close beads, run quality gates, commit, and push as part of session close. A current "do not commit" or "do not push" instruction still wins.

## Session Completion

This protocol applies when ending a Beads implementation workflow. It is subordinate to explicit user, repository, and orchestrator instructions.

1. **File issues for remaining work** - Create beads for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **Handle git/sync by active profile**:
   ```bash
   # Conservative/minimal/default: report status and proposed commands; wait for approval.
   git status

   # Team-maintainer opt-in only, unless current instructions forbid it:
   git pull --rebase
   git push
   git status
   ```
5. **Hand off** - Summarize changes, validation, issue status, and any blocked sync/commit/push step

**Critical rules:**
- Explicit user or orchestrator instructions override this Beads block.
- Do not commit or push without clear authority from the active profile or the current user request.
- If a required sync or push is blocked, stop and report the exact command and error.
<!-- END BEADS INTEGRATION -->
