# Multiserver setup with Docker Compose

Two server manager instances (`sm1`, `sm2`) from one image. The Assetto Corsa server is installed once with steamcmd
and every instance gets its own copy of it, so nothing has to be copied by hand.

| | sm1 | sm2 |
|---|---|---|
| Web UI | http://localhost:8772 | http://localhost:8773 |
| acServer UDP/TCP | 9600 | 9601 |
| acServer HTTP | 8081 | 8082 |

## Setup

Run everything from this directory.

1. Edit `sm1.config.yml` and `sm2.config.yml`: set a different random `session_key` and your own
   `admin_password_override`.
2. Install the AC server (once). steamcmd asks for the password and the Steam Guard code on the terminal; use an
   account that owns Assetto Corsa:

   ```
   docker compose --profile install run --rm install
   ```

   `STEAM_USER` can be set in the environment, otherwise the script asks for it. Re-running the command updates the
   pristine copy in `ac-base` only: an instance keeps its own copy until its `sm<n>-assetto` volume is deleted
   (which also removes its `cfg` and `results`).
3. Start: `docker compose up -d --build`. Behind a TLS-intercepting proxy, set `EXTRA_CA_CERT` to the root
   certificate before building (see `Dockerfile`).
4. In **Server Options** of each instance set the acServer ports from the table above. Compose publishes them 1:1,
   because the acServer announces the configured ports to clients.

## What is shared

- `ac-content`: cars, tracks, ... (`content/`). Uploading a car through one instance makes it available on both
  (`scan_content_folder_for_changes` picks it up within seconds).
- `shared`: accounts, championships, race weekends, entrants and custom races (`store.shared_data_path`, needs the
  json store).

Everything else (server options, `cfg/`, `results/`, logs) is per instance and lives in `sm<n>-assetto` / `sm<n>-data`.

## Adding an instance

Copy the `sm2` and `sm2-seed` services and the `sm2-*` volumes in `compose.yaml` and `sm2.config.yml`, and choose new
host ports.

## Reset

`docker compose --profile install down -v` deletes all volumes, including the AC server install.
