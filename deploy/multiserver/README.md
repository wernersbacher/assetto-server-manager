# Multiserver setup with Docker Compose

ONE Server Manager (one UI, one login, one database) which hosts two Assetto Corsa servers, `a` and `b`, so that e.g.
a championship can run on one server while a custom race runs on the other. The AC server is installed once with
steamcmd and every server gets its own copy of it (`cfg/`, `results/`, ...), so nothing has to be copied by hand.

| Default port | Server A | Server B |
|---|---|---|
| Web UI (both servers) | 8772 (`SM_WEB_PORT`) | |
| acServer UDP/TCP | 9600 (`SERVER_A_PORT`) | 9601 (`SERVER_B_PORT`) |
| acServer HTTP | 8081 (`SERVER_A_HTTP_PORT`) | 8082 (`SERVER_B_HTTP_PORT`) |

The published ports can be changed in a `.env` file (see "Changing the ports").

## Setup

Run everything from this directory.

1. Edit `config.yml`: set a random `session_key` and your own `admin_password_override`.
2. Install the AC server (once). steamcmd asks for the password and the Steam Guard code on the terminal; use an
   account that owns Assetto Corsa:

   ```
   docker compose --profile install run --rm install
   ```

   `STEAM_USER` can be set in the environment, otherwise the script asks for it. Re-running the command updates the
   pristine copy in `ac-base` only: a server keeps its own copy until its `assetto-a` / `assetto-b` volume is deleted
   (which also removes its `cfg` and `results`).
3. Start: `docker compose up -d --build`. Behind a TLS-intercepting proxy, set `EXTRA_CA_CERT` to the root
   certificate before building (see `Dockerfile`). The same variable is used by the `install` service: it installs
   the certificate in the container before steamcmd runs, so set it before step 2 as well. On Windows (PowerShell):
   `$env:EXTRA_CA_CERT = "$HOME\certs\vector-root-ca-2.0.pem"` for the current session, or set it as a user
   environment variable (then open a new terminal).
4. Log in and open **Server Options** of each server (the selector in the top bar switches the server). A new server
   starts with the default ports 9600 / 8081, so set server B to UDP/TCP port 9601 and HTTP port 8082 (or the ports
   from your `.env`, see "Changing the ports"). Compose publishes the ports 1:1, because the acServer announces the
   configured ports to clients. Plugin ports (stracker,
   KissMyRank, Real Penalty) have to be different per server too: Server Manager warns about conflicts and refuses to
   start an event on ports that a running server already uses. Publish the plugin ports in `compose.yaml` if you use
   them.

## Changing the ports

Copy `.env.example` to `.env` (gitignored) and change the values; compose reads it automatically. Unset variables
use the defaults of the table above.

| Variable | Default | |
|---|---|---|
| `SM_WEB_PORT` | 8772 | host port of the web UI |
| `SERVER_A_PORT` / `SERVER_A_HTTP_PORT` | 9600 / 8081 | acServer UDP+TCP / HTTP port of server a |
| `SERVER_B_PORT` / `SERVER_B_HTTP_PORT` | 9601 / 8082 | acServer UDP+TCP / HTTP port of server b |

- The acServer ports are published with the same number on host and container side, so after changing them set the
  **same ports in Server Options of each server** (a and b). Every new server starts with 9600 / 8081 in its options,
  so this is needed at least once per server.
- Only the host side of the web port changes. If you change `SM_WEB_PORT`, also change `server_manager_base_URL` in
  `config.yml` to the new port: it is used for links in chat messages, iCal events and the API.
- Run `docker compose up -d` again after changing `.env`.

## Volumes

- `ac-base`: the pristine install, only written by the `install` service.
- `assetto-a`, `assetto-b`: the install copy of each server (`cfg/`, `results/`, ...).
- `ac-content`: cars, tracks, ... It is mounted onto `content` of **both** servers. Content is managed (upload,
  delete) through the first server, so the other servers must see the same folder; keep this when adding a server.
- `data`: the Server Manager working directory (database, logs, sessions).

Results of both servers are shown together in the results list (filterable by server).

## Adding a server

Add a volume `assetto-c` and a `seed-c` service like `seed-a` to `compose.yaml`, mount the volume and `ac-content`
(onto `assetto-c/content`) into `server-manager`, publish new ports, and add a `servers:` entry to `config.yml`.

## Troubleshooting

If the install fails (for example with `Failed to write file after download` or `Missing file permissions`), run it
with `STEAM_STRACE=1`. The script then installs `strace` in the container, runs steamcmd under it and, on failure,
prints the failed file creations (`... = -1 ENOENT`, i.e. a missing directory) plus a directory listing of
`/root/Steam`, `/opt/steamcmd` and `/base`. Only file syscalls are traced (no read/write), so the password is not
part of the output. In PowerShell:

```
docker compose --profile install run --rm -e STEAM_STRACE=1 install
```

## Reset

`docker compose --profile install down -v` deletes all volumes, including the AC server install.
