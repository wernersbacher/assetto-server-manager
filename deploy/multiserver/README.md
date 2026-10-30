# Multiserver setup with Docker Compose

ONE Server Manager (one UI, one login, one database) which hosts two Assetto Corsa servers, `a` and `b`, so that e.g.
a championship can run on one server while a custom race runs on the other. The AC server is installed once with
steamcmd and every server gets its own copy of it (`cfg/`, `results/`, ...), so nothing has to be copied by hand.

| | Server A | Server B |
|---|---|---|
| Web UI (both servers) | http://localhost:8772 | |
| acServer UDP/TCP | 9600 | 9601 |
| acServer HTTP | 8081 | 8082 |

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
   certificate before building (see `Dockerfile`).
4. Log in and open **Server Options** of each server (the selector in the top bar switches the server). A new server
   starts with the default ports 9600 / 8081, so set server B to UDP/TCP port 9601 and HTTP port 8082. Compose
   publishes the ports 1:1, because the acServer announces the configured ports to clients. Plugin ports (stracker,
   KissMyRank, Real Penalty) have to be different per server too: Server Manager warns about conflicts and refuses to
   start an event on ports that a running server already uses. Publish the plugin ports in `compose.yaml` if you use
   them.

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

## Reset

`docker compose --profile install down -v` deletes all volumes, including the AC server install.
