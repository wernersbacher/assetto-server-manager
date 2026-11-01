ACPitwall
======================

A web interface to manage an Assetto Corsa Server.

ACPitwall is based on [JustaPenguin/assetto-server-manager](https://github.com/JustaPenguin/assetto-server-manager) (MIT license, see [LICENSE](LICENSE)) and is developed further in this repository.

## Features

* Quick Race Mode
* Custom Race Mode with saved presets
* Live Timings for current sessions
* Results pages for all previous sessions, with the ability to apply penalties
* Content Management - Upload tracks, weather and cars
* Sol Integration - Sol weather is compatible, including 24 hour time cycles (session start may advance/reverse time really fast before it syncs up - requires drivers to launch from content manager)
* Championship mode - configure multiple race events and keep track of driver, class and team points
* Race Weekends - a group of sequential sessions that can be run at any time. For example, you could set up a Qualifying session to run on a Saturday, then the Race to follow it on a Sunday. ACPitwall handles the starting grid for you, and lets you organise Entrants into splits based on their results and other factors!
* Automatic event looping
* Server Logs / Options Editing
* Accounts system with different permissions levels
* Linux and Windows Support!

## Installation


### Manual

1. Download the latest release from the [releases page](https://github.com/wernersbacher/ACPitwall/releases)
2. Extract the release
3. Edit the config.yml to suit your preferences
4. Either:
   - Copy the server folder from your Assetto Corsa install into the directory you configured in config.yml, or
   - Make sure that you have [steamcmd](https://developer.valvesoftware.com/wiki/SteamCMD) installed and in your $PATH 
     and have configured the steam username and password in the config.yml file.
5. Start the server using `./acpitwall` (on Linux) or by running `acpitwall.exe` (on Windows)


### Docker

A docker image is available under the name `ghcr.io/wernersbacher/acpitwall`. We recommend using docker-compose
to set up a docker environment for ACPitwall. This docker image has steamcmd pre-installed.
The image is built and published by GitHub Actions (`.github/workflows/ci.yml`): `latest` and `x.y.z` for every `vX.Y.Z` tag, `unstable` for the `main` branch.

See [Manual](#Manual) to set up ACPitwall without Docker. For ONE ACPitwall with several AC servers (shared
content, AC server installed once via steamcmd) see [Several servers](#several-servers) and [deploy/multiserver](deploy/multiserver/README.md).

**Note**: if you are using a directory volume for the server install (as is shown below), be sure to make 
the directory before running `docker-compose up` - otherwise its permissions may be incorrect. 

You will need a [config.yml](https://github.com/wernersbacher/ACPitwall/blob/main/cmd/acpitwall/config.example.yml) file to mount into the docker container.

An example docker-compose.yml looks like this:

```yaml
version: "3"

services:
  acpitwall:
    image: ghcr.io/wernersbacher/acpitwall:latest
    ports:
    # the port that ACPitwall runs on
    - "8772:8772"
    # the port that the assetto server runs on (may vary depending on your configuration inside ACPitwall)
    - "9600:9600"
    - "9600:9600/udp"
    # the port that the assetto server HTTP API runs on.
    - "8081:8081"
    # you may also wish to bind your configured UDP plugin ports here. 
    volumes: 
    # volume mount the entire server install so that 
    # content etc persists across restarts
    - ./server-install:/home/assetto/acpitwall/assetto
    
    # volume mount the config
    - ./config.yml:/home/assetto/acpitwall/config.yml
```

### Several servers

One ACPitwall can run several Assetto Corsa servers at once (e.g. a Championship event on one and a custom race on
the other), with one UI, login and database. List the servers in `config.yml`; without a `servers:` list one server is
used, from `steam.install_path`:

```yaml
steam:
  install_path: assetto-a     # the first server; this is where a missing install is fetched with steamcmd

servers:
  - id: a                     # the server id: used in the server query parameter and the API
    name: Server A
    install_path: assetto-a   # the working directory of this acServer (cfg/, results/, setups/, ...)
  - id: b
    name: Server B
    install_path: assetto-b
    # optional: executable_path (default steam.executable_path) and plugins (default server.plugins)
```

- **Content is shared**: cars, tracks and weather are managed (uploaded, deleted) through the install path of the
  *first* server. The `content` folder of every other server has to be that same folder (docker volume, junction on
  Windows, symlink on Linux), otherwise the other servers do not find uploaded cars and tracks.
- **Selecting a server**: with more than one server the top bar has a server selector (a `server=<id>` query parameter,
  remembered in the session), and the home page shows a card per server. Live timing, race control, Server Options and
  the plugin options (stracker, KissMyRank, Real Penalty) are per server.
- **Ports**: every server stores its own ports in its Server Options and starts with the default ports (9600 / 8081),
  so change them for each additional server (e.g. 9601 / 8082) and publish them in Docker. Plugin ports must differ
  between servers too. ACPitwall warns about conflicts and refuses to start an event on ports that a running server
  already uses.
- **Starting and scheduling**: events start on the selected server; scheduling forms have a "Server" field
  (`event-schedule-server`) to pick the server an event is scheduled for. A server which already runs an event rejects a
  new start. A Championship or Race Weekend can be active on one server only at a time.
- **Results** of all servers are listed together; the list can be filtered by server and shows a server badge.

A ready-made Docker Compose setup with two servers is in [deploy/multiserver](deploy/multiserver/README.md).

### Install on a VPS (Debian, Docker, Caddy)

A complete install of ACPitwall with two Assetto Corsa servers on a Debian VPS, with the web UI behind a Caddy reverse
proxy (automatic HTTPS) on your own domain. The details of every step are in
[deploy/multiserver](deploy/multiserver/README.md).

1. **DNS**: point an A record (and AAAA, if you use IPv6) of your domain, e.g. `acpitwall.example.com`, to the VPS.
   With Cloudflare use "DNS only", players connect to the game ports directly.
2. **Docker and git**:

   ```bash
   curl -fsSL https://get.docker.com | sh && sudo apt-get install -y git
   ```

3. **Get the code and build the image** (a few minutes; building on the VPS needs no registry):

   ```bash
   git clone https://github.com/wernersbacher/ACPitwall.git
   cd ACPitwall/deploy/multiserver
   docker compose build
   ```

4. **Configure** `config.yml`: a random `http.session_key` (`openssl rand -hex 32`), your own
   `accounts.admin_password_override` and `http.server_manager_base_URL: https://acpitwall.example.com` (no port).
   Keep `http.tls.enabled: false`, Caddy terminates TLS. Then `cp .env.example .env` and set `SM_WEB_BIND=127.0.0.1`
   in it, so the web UI is only reachable through Caddy.
5. **Install the Assetto Corsa server** into the volume. steamcmd (`docker compose --profile install run --rm install`)
   is not reliable; the verified way is to download "Assetto Corsa Dedicated Server" (Steam app 302550) on a Windows
   machine, copy the folder to the VPS (`scp -r C:\ac-server user@vps:~/ac-server`) and run
   `sh import-server.sh ~/ac-server` in `deploy/multiserver`.
6. **Start**:

   ```bash
   docker compose up -d
   docker compose logs -f acpitwall
   ```

7. **Caddy** (installed on the host): add this to the Caddyfile and run `sudo systemctl reload caddy`. Caddy fetches the
   certificate itself; websockets (live timing) work without extra settings.

   ```caddyfile
   acpitwall.example.com {
       reverse_proxy 127.0.0.1:8772
   }
   ```

8. **Firewall**: open 80 and 443 (tcp) for Caddy and, for the AC servers, 9600 and 9601 (tcp and udp) and 8081 and
   8082 (tcp). Keep 8772 closed. Docker bypasses `ufw` for published ports, which is why step 4 binds the web UI to
   `127.0.0.1`.
9. **First steps**: open `https://acpitwall.example.com`, log in as `admin`, set the ports of each server under
   Server > Options (A: 9600 / 8081, B: 9601 / 8082) and upload your cars and tracks.

To update later: `git pull`, `docker compose build`, `docker compose up -d` in `deploy/multiserver`. The volumes
(content, database, server copies) are kept. This setup is not verified on a VPS yet.

### Post Installation

We recommend uploading your entire Assetto Corsa `content/tracks` folder to get the full features of ACPitwall. 
This includes things like track images, all the correct layouts and any mod tracks you may have installed.

Also, we recommend installing Sol locally and uploading your Sol weather files to ACPitwall as well so you can try out Day/Night cycles and cool weather!

### Updating

Follow the steps below to update ACPitwall:

1. Back up your current ACPitwall database and config.yml.
2. Download the [latest version of ACPitwall](https://github.com/wernersbacher/ACPitwall/releases)
3. Extract the zip file.
4. Open the Changelog, read the entries between your current version and the new version. 
   There may be configuration changes that you need to make!
5. Make any necessary configuration changes.
6. Find ACPitwall executable for your operating system. Replace your current ACPitwall
   executable with it.
7. Start the new ACPitwall executable.

## Build From Source Process

See [DEVELOPMENT.md](DEVELOPMENT.md) for building ACPitwall from source and running it locally, natively or with
Docker, without needing a real Assetto Corsa server.

## API

ACPitwall has a read-only JSON API (championships and their standings, results, lap records, leaderboards, driver
profiles, server status, upcoming races) and a few endpoints used by the web interface. It follows the same access
rules as the pages it belongs to (session cookie of a logged in account, anonymous read access only if the server is
open), all times and durations are in milliseconds.

The API is described by an OpenAPI 3 document, which is the single source of truth, so it is not repeated here:

- `/api/openapi.yaml`: the specification (source: [internal/apidocs/openapi.yaml](internal/apidocs/openapi.yaml))
- `/api/docs`: Swagger UI for it, embedded in ACPitwall (works without internet access)

When you add or change an `/api` route, update `internal/apidocs/openapi.yaml` and run `go generate ./...`; a test
fails if the router and the specification differ.

## Credits & Thanks

ACPitwall would not have been possible without the following people:

* Henry Spencer - [Twitter](https://twitter.com/HWSpencer) / [GitHub](https://github.com/Hecrer)
* Callum Jones - [Twitter](https://twitter.com/icj_) / [GitHub](https://github.com/cj123)
* Joseph Elton
* The Pizzabab Championship
* The authors of [JustaPenguin/assetto-server-manager](https://github.com/JustaPenguin/assetto-server-manager), on which ACPitwall is based
* [ACServerManager](https://github.com/Pringlez/ACServerManager) and its authors, for 
inspiration and reference on understanding the AC configuration files

## Screenshots

Check out the [screenshots folder](https://github.com/wernersbacher/ACPitwall/tree/main/misc/screenshots)!
