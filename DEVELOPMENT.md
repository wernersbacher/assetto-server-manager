# Development

ACPitwall can be run locally without Steam or a real Assetto Corsa server. `dev/assetto-seed` contains a fake
AC install (two cars, two tracks, some results). Because it contains a `system/` folder, ACPitwall skips the
steamcmd install on startup. Starting an actual race doesn't work, as there is no `acServer` executable.

In both setups you log in at http://localhost:8772 as `admin` / `admin` (set via `accounts.admin_password_override`
in `dev/config.yml`).

## Native (Windows)

Requirements: Go (e.g. `scoop install go`) and Node.js >= 22.12 with npm (24 LTS recommended).

```powershell
./dev/run.ps1             # build frontend (first time only), generate embedded assets, build and start
./dev/run.ps1 -Frontend   # also rebuild the frontend
./dev/run.ps1 -Reset      # throw away local data (database, fake install, config.yml) and start fresh
./dev/run.ps1 -Seed      # also fill the database with test data (see below), then start
./dev/run.ps1 -Mock race # show a frozen mock live timing session (race|qualifying|practice), see below
./dev/run.ps1 -NoRun      # only build
```

The server runs in `cmd/acpitwall` with `FILESYSTEM_HTML=true`, so templates in `cmd/acpitwall/views` and
files in `cmd/acpitwall/static` are served from disk: template changes only need a browser refresh. Go changes
need a restart of the script.

On first run, `dev/config.yml` is copied to `cmd/acpitwall/config.yml` and `dev/assetto-seed` to
`cmd/acpitwall/assetto` (server a) and `cmd/acpitwall/assetto-b` (server b), all gitignored. Edit those copies to
try things locally.

The dev setup runs **two servers** (`servers:` in `dev/config.yml`, ids `a` and `b`) to try the multiserver features.
The `content` folder of `assetto-b` is a junction to that of `assetto` (content is managed through the first server), and
the sample results belong to server a. Both servers start with the default acServer ports; change them in Server Options
of a server if you want both to run. A `config.yml` and fake install from before this change have a single server: use
`./dev/run.ps1 -Reset` (it removes both fake installs) to get two.

### Test data

`-Seed` runs `cmd/utils/dev-seed` (also usable on its own: `go run ./cmd/utils/dev-seed -dir cmd/acpitwall`),
which fills the local dev instance (for the first server, a) with 10 drivers, ~18 result files (practice/qualify/race on `dev_ring` gp/short and
`dev_track`), the championships "Dev Cup" (2 classes, drop worst result, championship race settings, 4 completed events
and 1 scheduled) and "Dev Sprint Series" (1 completed, 2 scheduled, one weekly), 3 custom races (starred, scheduled,
weekly) and a race weekend. It prints the championship standings as a sanity check. The server must not be running (the
database is locked otherwise). Running it again does nothing; `-reset` (tool flag) removes the seeded data and seeds again,
`./dev/run.ps1 -Reset -Seed` starts from a fresh database. The admin account is not touched.

`-Mock <race|qualifying|practice>` additionally starts a made-up event on every server (a: the chosen session, the others: a practice; the process is replaced by one which starts no acServer, so the servers show as running) and fills the live timing of server a with a frozen made-up session (the same
drivers as the seed, a few laps each with cuts and splits, positions on the live map, two collisions, chat, one driver who
has left), by replaying UDP messages into the race control (`internal/racecontrol/mock.go`, switched on by the
environment variable `DEV_MOCK_LIVE_TIMING`). Use `-Seed -Mock race` to see results, championships and leaderboards
and live timing filled at once. The mock drivers stay connected, but nothing moves.

### Frontend

TypeScript and SCSS live in `cmd/acpitwall/typescript` and are built into `cmd/acpitwall/static`:

```powershell
cd cmd/acpitwall/typescript
npm ci
npx gulp build   # one-off build
npx gulp         # build, then watch for changes
```

## Docker

```powershell
docker compose up --build   # http://localhost:8772
docker compose down -v      # stop and delete all data
```

`compose.yaml` builds the production `Dockerfile` and runs it with `dev/config.yml` and the fake AC installs of both servers (the content folder of b is a symlink to that of a). The data
lives in Docker volumes, so it's independent from the native setup.

Behind a TLS-intercepting proxy, the build needs the proxy's root CA. It is passed as a BuildKit secret, so it is not
stored in the image:

```powershell
$env:EXTRA_CA_CERT = "C:\path\to\corporate-root.crt"
docker compose build
# or without compose:
docker build --secret id=ca_cert,src=C:\path\to\corporate-root.crt -t acpitwall .
```

## Tests

```powershell
go test ./...
```
