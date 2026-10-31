Assetto Server Manager
======================

A web interface to manage an Assetto Corsa Server.

## Features

* Quick Race Mode
* Custom Race Mode with saved presets
* Live Timings for current sessions
* Results pages for all previous sessions, with the ability to apply penalties
* Content Management - Upload tracks, weather and cars
* Sol Integration - Sol weather is compatible, including 24 hour time cycles (session start may advance/reverse time really fast before it syncs up - requires drivers to launch from content manager)
* Championship mode - configure multiple race events and keep track of driver, class and team points
* Race Weekends - a group of sequential sessions that can be run at any time. For example, you could set up a Qualifying session to run on a Saturday, then the Race to follow it on a Sunday. Server Manager handles the starting grid for you, and lets you organise Entrants into splits based on their results and other factors!
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
5. Start the server using `./server-manager` (on Linux) or by running `server-manager.exe` (on Windows)


### Docker

A docker image is available under the name `seejy/assetto-server-manager`. We recommend using docker-compose
to set up a docker environment for the server manager. This docker image has steamcmd pre-installed.

See [Manual](#Manual) to set up server manager without Docker. For ONE server manager with several AC servers (shared
content, AC server installed once via steamcmd) see [Several servers](#several-servers) and [deploy/multiserver](deploy/multiserver/README.md).

**Note**: if you are using a directory volume for the server install (as is shown below), be sure to make 
the directory before running `docker-compose up` - otherwise its permissions may be incorrect. 

You will need a [config.yml](https://github.com/wernersbacher/ACPitwall/blob/master/cmd/server-manager/config.example.yml) file to mount into the docker container.

An example docker-compose.yml looks like this:

```yaml
version: "3"

services:
  server-manager:
    image: seejy/assetto-server-manager:latest
    ports:
    # the port that the server manager runs on
    - "8772:8772"
    # the port that the assetto server runs on (may vary depending on your configuration inside server manager)
    - "9600:9600"
    - "9600:9600/udp"
    # the port that the assetto server HTTP API runs on.
    - "8081:8081"
    # you may also wish to bind your configured UDP plugin ports here. 
    volumes: 
    # volume mount the entire server install so that 
    # content etc persists across restarts
    - ./server-install:/home/assetto/server-manager/assetto
    
    # volume mount the config
    - ./config.yml:/home/assetto/server-manager/config.yml
```

### Several servers

One Server Manager can run several Assetto Corsa servers at once (e.g. a Championship event on one and a custom race on
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
  between servers too. Server Manager warns about conflicts and refuses to start an event on ports that a running server
  already uses.
- **Starting and scheduling**: events start on the selected server; scheduling forms have a "Server" field
  (`event-schedule-server`) to pick the server an event is scheduled for. A server which already runs an event rejects a
  new start. A Championship or Race Weekend can be active on one server only at a time.
- **Results** of all servers are listed together; the list can be filtered by server and shows a server badge.

A ready-made Docker Compose setup with two servers is in [deploy/multiserver](deploy/multiserver/README.md).

### Post Installation

We recommend uploading your entire Assetto Corsa `content/tracks` folder to get the full features of Server Manager. 
This includes things like track images, all the correct layouts and any mod tracks you may have installed.

Also, we recommend installing Sol locally and uploading your Sol weather files to Server Manager as well so you can try out Day/Night cycles and cool weather!

### Updating

Follow the steps below to update Server Manager:

1. Back up your current Server Manager database and config.yml.
2. Download the [latest version of Server Manager](https://github.com/wernersbacher/ACPitwall/releases)
3. Extract the zip file.
4. Open the Changelog, read the entries between your current version and the new version. 
   There may be configuration changes that you need to make!
5. Make any necessary configuration changes.
6. Find the Server Manager executable for your operating system. Replace your current Server Manager
   executable with it.
7. Start the new Server Manager executable.

## Build From Source Process

See [DEVELOPMENT.md](DEVELOPMENT.md) for building Server Manager from source and running it locally, natively or with
Docker, without needing a real Assetto Corsa server.

## API

Championships can be read as JSON, e.g. to show the schedule on another website. The API follows the same access rules
as the championship pages, so for anonymous access the server has to allow public read access.

- `GET /api/championships`: all championships
- `GET /api/championship/{id}`: a single championship

Each championship contains its classes and cars, and its events with track, schedule (`Scheduled`, an optional
`Recurrence` rule) and sessions. A session has a length in minutes (`Time`) or `Laps`, and a calculated `Start` if
it is known. `Status` is one of `unscheduled`, `scheduled`, `in_progress` or `completed`. Race Weekend events have
a `RaceWeekendName`, and each of their sessions has its own schedule and track. Times are in RFC 3339 format.
Entrants, sign up responses and passwords are not included.
`DropWorstResults` is the number of worst event results that are dropped per driver in the standings (0 means off).
Standings themselves are not part of the API yet.

Results can be read in the same way, with the same access rules as the results pages:

- `GET /api/results`: a page of results, newest first. It takes the same query parameters as the results list:
  `page` (starting at 0, 25 results per page, a page past the end is a 404), `type` (`practice`, `qualify`, `race` or
  `booking`), `track` and `driver` (name or GUID). The response has the `Page` and the total number of `Pages`, and
  the `Results`: file name, `Date`, `SessionType`, track (`Track`, `TrackLayout` and a readable `TrackName`) and
  the `Drivers`.
- `GET /api/results/{fileName}`: a single result, with or without the `.json` extension. It has the same fields as the
  list, plus the `Classification` (position, driver name and GUID, car, team, best lap, total time, laps, penalties and
  disqualification) and all `Laps` (lap time, sectors, cuts, tyre).
- `GET /api/track-records`: the lap records of a track layout. The query parameter `track` (the track folder name) is
  required, `layout` (the layout folder name, empty or missing for the default layout) selects the layout, `car` restricts
  the records to one car model and `filterServer` (see below) to one server; invalid names are a 400. Only valid laps
  (no cuts) from the results count. The response has the track (`Track`, `TrackLayout`, a readable `TrackName`), the
  `Records`, which are the best lap of every car, fastest first, and the `Overall` record (the first one, `null` if
  there are none). A record has the driver (`DriverName`, `DriverGUID`), the car (`CarModel`, `CarName`), `LapTime` and
  `Sectors` in milliseconds, the `Tyre`, the `Date` and `SessionType` of the session, its results `FileName` (see
  `/api/results/{fileName}`) and, with several servers, the `Server` (`ID`, `Name`).
- `GET /api/leaderboard`: the hotlap leaderboard of a track layout. `track` is required (the track folder name), `layout`
  selects the layout (empty or missing is the default layout), `car` (repeatable) restricts it to car models, `period`
  is `all` (default), `30d` or `7d`, `sessionType` is `practice`, `qualify` or `race` (empty for any), `perCar=1` lists
  the best lap of every driver and car instead of one per driver and `filterServer` (see below) restricts it to one
  server; invalid values are a 400. Only valid laps (no cuts) count. The response repeats the request (`Track`,
  `TrackLayout`, a readable `TrackName`, `Period`, `SessionType`, `PerCar`, `Cars`) and has the `Entries`, fastest first:
  `Position`, `DriverName`, `DriverGUID`, `CarModel`, `CarName`, `LapTime`, `Gap` to the leader and `Sectors` in
  milliseconds, `Tyre`, `Date`, `SessionType`, the results `FileName` and, with several servers, the `Server` (`ID`,
  `Name`). The page is `/leaderboards`.
- `GET /api/driver/{guid}`: the profile of a driver (404 if the GUID is in no results). It has the `Name` last used and
  all `Names`, `Nation` and `Team`, the number of `Sessions` by type, `Races`, `Wins`, `Podiums`, `Laps`, `ValidLaps`,
  `Cuts`, `CollisionsCar` and `CollisionsEnv`, the `PersonalBests` (best valid lap per track layout and car: `Track`,
  `TrackLayout`, `CarModel`, `LapTime` and `Sectors` in milliseconds, `Tyre`, `Date`, `SessionType`, results `FileName`),
  all `Results` (newest first: `FileName`, `Date`, track, `SessionType`, `Cars`, `Position` in a race, `Disqualified`,
  `BestLap`, `Laps`) and the `Championships` the driver is in (`ID`, `Name`, `Class`, `Position`, `Entrants`, `Points`,
  `Completed`) of the selected server. `filterServer` (see below) restricts `Results` and `PersonalBests` to one server;
  with several servers they have a `Server` (`ID`, `Name`). The page is `/driver/{guid}`; driver names in results and
  championship tables link to it.

The three driver statistics APIs (`/api/track-records`, `/api/driver/{guid}`, `/api/leaderboard`) and their pages need a
logged in account with read access, even on an open Server Manager, unless "Public driver statistics" is enabled on the
Accounts page (default off). Anonymous requests are redirected to `/login` (302), like on other protected APIs.

All times and durations are in milliseconds, as Assetto Corsa stores them. Driver names are shortened if the server
hides driver last names. Collisions are not included, use `/results/download/{fileName}` for the full results file.

The state of the server can be read from `GET /api/server-status`. It follows the same access rules as the live timing
page, so it is not available in performance mode. It reports whether an event is `Running` and, if so, the server and
event name, whether it is a championship or race weekend (with its name), the track, the session that is currently
running (type, name, length in minutes or laps, `Started`), `MaxClients` and the connected drivers with their cars.
Passwords, driver GUIDs and IP addresses are not included.

The scheduled events shown in the calendar can be read from `GET /api/upcoming-races`, with the same access rules as the
calendar. It lists the custom races, championship events and race weekend sessions that start from now on, soonest
first. The optional query parameter `limit` (a positive number) caps the number of entries. Each entry has a `Kind`
(`custom_race`, `championship_event` or `race_weekend_session`), the `Start` time, the `Name`, the championship
(`ChampionshipID`, `ChampionshipName`) or race weekend (`RaceWeekendID`, `RaceWeekendName`) it belongs to, the track,
the `Cars` and the `Sessions` with their length and calculated `Start`. `OnThisServer` is false if the event is
scheduled on another server of a multiserver setup. Recurring events are not expanded: they are listed once with their
next `Start` and the `Recurrence` rule. Entrants and passwords are not included.

With several servers (see [Several servers](#several-servers)) the APIs know about them:

- `GET /api/servers`: all servers (`ID`, `Name`), whether an event is `Running` and, if so, its `EventType`
  (`Championship`, `Race Weekend`, `Looping event`, `Time attack`, `Running`, `... practice`) and `EventName`, and the number
  of `DriversOnline`. It does not depend on a selected server. Same access rules as `/api/server-status` (the live timing page).
- `GET /api/server-status` reports the server given in the `server` query parameter (the server id), the first server
  without it (or the one remembered in the session).
- `filterServer=<server id>` restricts `/api/results`, `/api/track-records`, `/api/leaderboard`, `/api/upcoming-races` (and the calendar and its .ics feed) to one
  server; an unknown id matches nothing.
- `/api/results` entries have a `Server` (`ID`, `Name`) and `/api/championships` has an `ActiveServer` while one of
  their events runs. `Server` is only set if there are several servers. `/api/upcoming-races` entries have a `ServerID`
  and, with several servers, a `ServerName`.

## Credits & Thanks

Assetto Corsa Server Manager would not have been possible without the following people:

* Henry Spencer - [Twitter](https://twitter.com/HWSpencer) / [GitHub](https://github.com/Hecrer)
* Callum Jones - [Twitter](https://twitter.com/icj_) / [GitHub](https://github.com/cj123)
* Joseph Elton
* The Pizzabab Championship
* [ACServerManager](https://github.com/Pringlez/ACServerManager) and its authors, for 
inspiration and reference on understanding the AC configuration files

## Screenshots

Check out the [screenshots folder](https://github.com/wernersbacher/ACPitwall/tree/master/misc/screenshots)!
