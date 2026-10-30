# Multiserver setup with Docker Compose

ONE Server Manager (one UI, one login, one database) which hosts two Assetto Corsa servers, `a` and `b`, so that e.g.
a championship can run on one server while a custom race runs on the other. The AC server is installed once into a
volume and every server gets its own copy of it (`cfg/`, `results/`, ...). The cars and tracks are shared.

This is a runbook from zero (fresh Linux VPS or a local Windows machine). Run all commands in this directory
(`deploy/multiserver`). At the end of each part it says what was verified; "not verified" means documented only.

| Default port | Server A | Server B |
|---|---|---|
| Web UI (both servers) | 8772 (`SM_WEB_PORT`) | |
| acServer UDP+TCP | 9600 (`SERVER_A_PORT`) | 9601 (`SERVER_B_PORT`) |
| acServer HTTP | 8081 (`SERVER_A_HTTP_PORT`) | 8082 (`SERVER_B_HTTP_PORT`) |

## 1. Prerequisites

- Docker Engine with the Compose plugin (`docker compose version`) and git.
  VPS (Debian/Ubuntu): `curl -fsSL https://get.docker.com | sh && sudo apt-get install -y git`
  (the Compose plugin comes with it; not verified on a VPS).
- Windows: Docker Desktop and git. Use PowerShell; Git Bash is only needed for `import-server.sh`.
- About 5 GB of disk for the image build and the volumes (more with many cars/tracks).
- To install the AC server you need either a Steam account that owns Assetto Corsa (steamcmd, part 4a) or the
  dedicated server folder from a Windows machine (manual import, part 4b).

## 2. Get the code and build the image

```bash
git clone https://github.com/wernersbacher/assetto-server-manager.git
cd assetto-server-manager/deploy/multiserver
docker compose build
```

The first build compiles the frontend and the Go binary (a few minutes). The image is called
`assetto-server-manager:local`.

Behind a TLS-intercepting corporate proxy the build and steamcmd need the proxy's root certificate (PEM): set
`EXTRA_CA_CERT` first. Bash: `export EXTRA_CA_CERT=$HOME/corp-root.pem`; PowerShell:
`$env:EXTRA_CA_CERT = "$HOME\certs\corp-root.pem"`. Without it the variable is ignored.

Verified on Windows (Docker Desktop): build and everything below except where marked.

## 3. Configure

1. `config.yml` (mounted read-only into the container):
   - `http.session_key`: a random secret, e.g. `openssl rand -hex 32` (the file contains the placeholder `devkey`).
   - `accounts.admin_password_override`: the password of the user `admin`. Change it. As the example config says, you
     can remove the line after the first login and set the password in the UI (see
     `cmd/server-manager/config.example.yml`).
   - `http.server_manager_base_URL`: the public URL of the web UI **including the published port**, e.g.
     `https://acsm.example.com` or `http://203.0.113.10:8772`. It is used for links in notifications, iCal events and
     the API. Leave `http.hostname: 0.0.0.0:8772` as it is (that is the container side).
   - `servers:` lists the two servers (`a` -> `assetto-a`, `b` -> `assetto-b`); `steam.install_path` stays equal to the
     first one. Keep `steam.username` / `steam.password` empty: the AC server is installed by part 4.
2. Ports (optional): `cp .env.example .env` and edit it (gitignored; compose reads it automatically). Variables:
   `SM_WEB_PORT`, `SERVER_A_PORT`, `SERVER_A_HTTP_PORT`, `SERVER_B_PORT`, `SERVER_B_HTTP_PORT`. Without a `.env` the
   defaults of the table above apply. The acServer ports are published **1:1** (host port == container port), because
   the acServer announces its configured ports to the lobby and the clients. If you change `SM_WEB_PORT`, change
   `server_manager_base_URL` too.

Verified: `docker compose config -q` is valid with and without `.env`.

## 4. Install the Assetto Corsa server into the `ac-base` volume

The volume `assetto-multiserver_ac-base` (`<compose project>_ac-base`) holds the pristine install. Use one of the two
ways. **Be aware: 4a is not reliable (known issue), 4b is the path that was verified.**

### 4a. steamcmd (try first on a VPS, it may work there)

```bash
docker compose --profile install run --rm install
```

It asks for the Steam username (or set `STEAM_USER`), the password and the Steam Guard code. The account has to own
Assetto Corsa; the dedicated server is Steam app 302550 ("Assetto Corsa Dedicated Server").

Known issue (bead `assetto-server-manager-d6x`, unresolved): on the development machine steamcmd in Docker logged in
but failed with `remotestoragefilesynccontext.cpp: Failed to write file after download (2)` and
`Failed to install app 302550 (Missing file permissions)`. The cause is not known; it was not the volume, the disk,
the network, the CA or the argument order. It is not known whether it also happens on a VPS (not verified there). If
it fails for you, use 4b.

Troubleshooting:
- `docker compose --profile install run --rm -e STEAM_STRACE=1 install` runs steamcmd under strace (only file syscalls,
  no password in the output) and on failure prints the failed file creations and directory listings.
- Repeated failed logins trigger Steam's rate limit: wait about 30 minutes.
- Steam Guard: approve the login / type the code when asked; a missing approval ends in the same
  `Missing file permissions` error.
- Behind a TLS-intercepting proxy set `EXTRA_CA_CERT` (part 2); the install service installs it before steamcmd runs.

### 4b. Manual import (verified)

1. Download the server on a Windows machine, either in the Steam client (Library, filter "Tools": "Assetto Corsa
   Dedicated Server") or with steamcmd.exe:

   ```powershell
   steamcmd.exe +force_install_dir C:\ac-server +login <user> +app_update 302550 +quit
   ```

   The folder contains `acServer` (the Linux binary), `acServer.exe`, `system/`, `cfg/`, `content/`, `results/`,
   `setups/`.
2. Copy the folder to the host. From Windows to a VPS (OpenSSH is part of Windows 10/11):

   ```powershell
   scp -r "C:\ac-server" user@vps:~/ac-server
   ```

   Or zip it, upload it and unzip it there. (Between Linux machines: `rsync -a ac-server/ user@vps:ac-server/`.)
3. Import it into the volume with the helper (the image has to be built, part 2):

   ```bash
   sh import-server.sh ~/ac-server
   ```

   `import-server.sh <folder> [--force]` checks `system/`, `content/` and that `acServer` is a Linux ELF binary,
   refuses a non-empty volume unless `--force` (which empties it first), copies the folder, sets `chmod +x acServer`
   and `chown assetto`, prints a summary and starts the acServer for 3 seconds (you should see
   `Assetto Corsa Dedicated Server v1.15` or similar). Environment: `AC_BASE_VOLUME` (target volume, default
   `assetto-multiserver_ac-base`; set it if you use another compose project name via `COMPOSE_PROJECT_NAME`) and
   `AC_IMAGE` (default `assetto-server-manager:local`).

   On Windows, directly from PowerShell without the script (plain bind mount, no path mangling):

   ```powershell
   docker run --rm --user root -v assetto-multiserver_ac-base:/base -v "C:\ac-server:/src:ro" --entrypoint sh assetto-server-manager:local -c 'cp -r /src/. /base/ && chmod +x /base/acServer && chown -R assetto:assetto /base'
   ```

   With Git Bash `sh import-server.sh /c/ac-server` also works (the script disables the path conversion).

Note: the downloaded `content/` holds the default cars and tracks with skins only (no `data/tyres.ini`), so the manager
logs `Could not update tyres for car ...` errors. They are harmless; upload the real content (part 6).

Verified on Windows (Docker Desktop): the script (empty volume, refusal of a non-empty one, `--force`, an invalid
folder, smoke check printing `Assetto Corsa Dedicated Server v1.15`), tested into a throwaway volume.

## 5. Start

```bash
docker compose up -d
docker compose logs -f server-manager
```

`seed-content`, `seed-a` and `seed-b` are one-shot jobs that copy the install into the volumes; they show
`Exited (0)` in `docker compose ps -a`. That is normal. The seed jobs never overwrite an existing server copy.

Open `http://localhost:8772` (or `http://<vps-ip>:<SM_WEB_PORT>`) and log in as `admin` with the
`admin_password_override` of `config.yml`.

## 6. First steps in the UI

1. **Set the ports of each server** (required). Open Server > Options (`/server-options`) and use the server selector
   in the top bar to switch between Server A and Server B. Every new server starts with the default ports 9600 / 8081,
   so set server A to the published ports (UDP port, TCP port and HTTP port, defaults 9600 / 8081) and server B to
   9601 / 8082 (or the values of your `.env`). Without it the acServer listens on ports that are not published
   (observed), and Server Manager warns about port conflicts between the servers (`Port 9600 (udp) is used by ... and
   by ...`; it also refuses to start an event on ports that a running server uses). Plugin ports (stracker,
   KissMyRank, Real Penalty) must differ per server as well; publish them in `compose.yaml` if you use them.
2. **Upload cars and tracks** on the Cars and Tracks pages (Content in the sidebar, "Upload cars" / "Upload
   tracks"). Content is managed through server A and is the same folder for both servers. The uploads are sent
   base64-encoded in one JSON request, so upload large packs in portions.
3. Create a championship or custom race and start it on a chosen server (the "Server" field of the schedule form).
4. Check the API: open `http://localhost:8772/api/servers` in the logged-in browser (same access rules as the live timing page): it lists
   the servers, whether an event runs and the drivers online.

Lobby: `RESPONSE: ERROR,INVALID SERVER,CHECK YOUR PORT FORWARDING SETTINGS` in the log is expected locally (NAT or
unreachable ports). On a VPS with open ports the server should register in the lobby (not verified).
The "Join with Content Manager" link needs outbound HTTPS; behind a TLS-intercepting proxy it fails with
`x509: certificate signed by unknown authority`. That is harmless.

## 7. Exposing it on a VPS

Host sysctl: at start the manager warns that `net.core.rmem_default`, `rmem_max`, `wmem_default` and `wmem_max` are
too small (recommended 2000000; shown while the live map is enabled, which it is in `config.yml`). A container cannot
set them, so on the VPS:

```bash
sudo tee /etc/sysctl.d/99-acsm.conf <<'EOF'
net.core.rmem_default=2000000
net.core.rmem_max=2000000
net.core.wmem_default=2000000
net.core.wmem_max=2000000
EOF
sudo sysctl --system
```

On Docker Desktop they stay small; ignore the warning there. (Not verified on a VPS.)

Firewall. Players need, for each server, the UDP and TCP game port and the HTTP port; the web UI port only for you:

```bash
sudo ufw allow 9600/udp && sudo ufw allow 9600/tcp && sudo ufw allow 8081/tcp   # server A
sudo ufw allow 9601/udp && sudo ufw allow 9601/tcp && sudo ufw allow 8082/tcp   # server B
sudo ufw allow 8772/tcp                                                         # web UI, see below
```

Docker publishes ports itself through iptables and bypasses the ufw rules, so ufw does not block published ports. To
restrict a port use the `DOCKER-USER` chain, or bind it to a local address in the `ports:` of `compose.yaml`.

Web UI: the UI has its own login, but do not expose port 8772 plainly (plain HTTP, passwords in clear text). Put a
TLS reverse proxy in front: change the `ports:` entry of the web UI in `compose.yaml` to
`"127.0.0.1:${SM_WEB_PORT:-8772}:8772"`, then e.g. Caddy (automatic Let's Encrypt certificate; the DNS name has to
point to the VPS and ports 80/443 must be open):

```
acsm.example.com {
    reverse_proxy 127.0.0.1:8772
}
```

and set `server_manager_base_URL: https://acsm.example.com` in `config.yml`. Change the default
`admin_password_override` first (and remove it after your first use, as the example config says). Not verified on a
VPS.

## 8. Operations

Volumes (prefix `assetto-multiserver_`): `ac-base` (pristine install), `ac-content` (cars/tracks of both servers),
`assetto-a` / `assetto-b` (`cfg`, `results`, ... of each server), `data` (database, logs, sessions).

Backup (the whole set; stop the manager first for a consistent copy):

```bash
docker compose stop server-manager
for v in ac-content assetto-a assetto-b data; do
  docker run --rm -v assetto-multiserver_$v:/v -v "$PWD":/b alpine tar czf /b/$v.tgz -C /v .
done
docker compose start server-manager
```

`ac-base` can be recreated by part 4. Restore into an empty volume, e.g.:

```bash
docker run --rm -v assetto-multiserver_data:/v -v "$PWD":/b alpine sh -c 'cd /v && tar xzf /b/data.tgz'
```

(In PowerShell use `${PWD}` for `$PWD`.) The files are owned by the `assetto` user (uid is preserved by tar).
Backup and restore are not verified.

Update the manager:

```bash
git pull
docker compose build
docker compose up -d
```

The data is kept; seed jobs never overwrite existing server copies.

Update the AC server: re-import it into `ac-base` (`sh import-server.sh <folder> --force`, or 4a). The servers keep
their own copies in `assetto-a` / `assetto-b`, so they do not change. Either copy the new files by hand into these
volumes, or delete the volumes (`docker compose down`, then `docker volume rm assetto-multiserver_assetto-a`; this
also deletes that server's `cfg` and `results`, so back them up first) and run `docker compose up -d` to re-seed.

Full reset: `docker compose --profile install down -v` deletes **all** volumes including the AC install. The install
has to be redone afterwards.

## Adding a server

Add a volume `assetto-c` and a `seed-c` service like `seed-a` to `compose.yaml`, mount the volume and `ac-content`
(onto `assetto-c/content`) into `server-manager`, publish new ports, and add a `servers:` entry to `config.yml`.

## Troubleshooting summary

- Port conflict warnings or an unreachable server: the ports in Server Options differ from the published ones (part 6, step 1).
- `Could not update tyres for car ...`: content from the Steam download has no `data/tyres.ini`; harmless (4b).
- Lobby `INVALID SERVER`: ports not reachable from the internet (6).
- steamcmd failure: 4a, `STEAM_STRACE=1`, otherwise 4b.
- UDP buffer warning: host sysctl (7).

See also the root [README](../../README.md#several-servers) and [DEVELOPMENT.md](../../DEVELOPMENT.md).
