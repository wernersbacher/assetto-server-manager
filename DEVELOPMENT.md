# Development

Server Manager can be run locally without Steam or a real Assetto Corsa server. `dev/assetto-seed` contains a fake
AC install (two cars, two tracks, some results). Because it contains a `system/` folder, Server Manager skips the
steamcmd install on startup. Starting an actual race doesn't work, as there is no `acServer` executable.

In both setups you log in at http://localhost:8772 as `admin` / `admin` (set via `accounts.admin_password_override`
in `dev/config.yml`).

## Native (Windows)

Requirements: Go (e.g. `scoop install go`) and Node.js with npm.

```powershell
./dev/run.ps1             # build frontend (first time only), generate embedded assets, build and start
./dev/run.ps1 -Frontend   # also rebuild the frontend
./dev/run.ps1 -Reset      # throw away local data (database, fake install, config.yml) and start fresh
./dev/run.ps1 -NoRun      # only build
```

The server runs in `cmd/server-manager` with `FILESYSTEM_HTML=true`, so templates in `cmd/server-manager/views` and
files in `cmd/server-manager/static` are served from disk: template changes only need a browser refresh. Go changes
need a restart of the script.

On first run, `dev/config.yml` is copied to `cmd/server-manager/config.yml` and `dev/assetto-seed` to
`cmd/server-manager/assetto` (both gitignored). Edit those copies to try things locally.

### Frontend

TypeScript and SCSS live in `cmd/server-manager/typescript` and are built into `cmd/server-manager/static`:

```powershell
cd cmd/server-manager/typescript
npm ci
npx gulp build   # one-off build
npx gulp         # build, then watch for changes
```

## Docker

```powershell
docker compose up --build   # http://localhost:8772
docker compose down -v      # stop and delete all data
```

`compose.yaml` builds the production `Dockerfile` and runs it with `dev/config.yml` and the fake AC install. The data
lives in Docker volumes, so it's independent from the native setup.

Behind a TLS-intercepting proxy, the build needs the proxy's root CA. It is passed as a BuildKit secret, so it is not
stored in the image:

```powershell
$env:EXTRA_CA_CERT = "C:\path\to\corporate-root.crt"
docker compose build
# or without compose:
docker build --secret id=ca_cert,src=C:\path\to\corporate-root.crt -t assetto-server-manager .
```

## Tests

```powershell
go test ./...
```
