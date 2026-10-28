Assetto Server Manager
======================

[![Build Status](https://travis-ci.org/JustaPenguin/assetto-server-manager.svg?branch=master)](https://travis-ci.org/JustaPenguin/assetto-server-manager) [![Discord](https://img.shields.io/discord/557940238991753223.svg)](https://discordapp.com/invite/6DGKJzB)

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
* Integration with [Assetto Corsa Skill Ratings](https://acsr.assettocorsaservers.com)!
* Automatic event looping
* Server Logs / Options Editing
* Accounts system with different permissions levels
* Linux and Windows Support!

**If you like Assetto Server Manager, please consider supporting us with a [donation](https://paypal.me/JustaPenguinUK)!**

## Installation


### Manual

1. Download the latest release from the [releases page](https://github.com/JustaPenguin/assetto-server-manager/releases)
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

See [Manual](#Manual) to set up server manager without Docker.

**Note**: if you are using a directory volume for the server install (as is shown below), be sure to make 
the directory before running `docker-compose up` - otherwise its permissions may be incorrect. 

You will need a [config.yml](https://github.com/JustaPenguin/assetto-server-manager/blob/master/cmd/server-manager/config.example.yml) file to mount into the docker container.

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

### Post Installation

We recommend uploading your entire Assetto Corsa `content/tracks` folder to get the full features of Server Manager. 
This includes things like track images, all the correct layouts and any mod tracks you may have installed.

Also, we recommend installing Sol locally and uploading your Sol weather files to Server Manager as well so you can try out Day/Night cycles and cool weather!

### Updating

Follow the steps below to update Server Manager:

1. Back up your current Server Manager database and config.yml.
2. Download the [latest version of Server Manager](https://github.com/JustaPenguin/assetto-server-manager/releases)
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

Results can be read in the same way, with the same access rules as the results pages:

- `GET /api/results`: a page of results, newest first. It takes the same query parameters as the results list:
  `page` (starting at 0, 25 results per page, a page past the end is a 404), `type` (`practice`, `qualify`, `race` or
  `booking`), `track` and `driver` (name or GUID). The response has the `Page` and the total number of `Pages`, and
  the `Results`: file name, `Date`, `SessionType`, track (`Track`, `TrackLayout` and a readable `TrackName`) and
  the `Drivers`.
- `GET /api/results/{fileName}`: a single result, with or without the `.json` extension. It has the same fields as the
  list, plus the `Classification` (position, driver name and GUID, car, team, best lap, total time, laps, penalties and
  disqualification) and all `Laps` (lap time, sectors, cuts, tyre).

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

## Credits & Thanks

Assetto Corsa Server Manager would not have been possible without the following people:

* Henry Spencer - [Twitter](https://twitter.com/HWSpencer) / [GitHub](https://github.com/Hecrer)
* Callum Jones - [Twitter](https://twitter.com/icj_) / [GitHub](https://github.com/cj123)
* Joseph Elton
* The Pizzabab Championship
* [ACServerManager](https://github.com/Pringlez/ACServerManager) and its authors, for 
inspiration and reference on understanding the AC configuration files

## Screenshots

Check out the [screenshots folder](https://github.com/JustaPenguin/assetto-server-manager/tree/master/misc/screenshots)!
