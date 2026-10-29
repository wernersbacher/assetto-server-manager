// dev-seed fills the local dev instance (see dev/run.ps1) with realistic test data: drivers, result files,
// championships, custom races and a race weekend, all on the fake cars and tracks of dev/assetto-seed.
//
// It opens the database directly, so the server must not be running. Seeding is idempotent: once seeded, running
// the tool again does nothing until it is run with -reset (which removes the seeded data and seeds again).
package main

import (
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"

	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	servermanager "github.com/wernersbacher/assetto-server-manager"

	"github.com/etcd-io/bbolt"
)

func main() {
	var dir string
	var reset bool

	flag.StringVar(&dir, "dir", "", "server manager working directory containing config.yml (default: . if it has a config.yml, otherwise cmd/server-manager)")
	flag.BoolVar(&reset, "reset", false, "remove previously seeded data, then seed again")
	flag.Parse()

	if err := run(dir, reset, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "dev-seed: "+err.Error())
		os.Exit(1)
	}
}

func resolveDir(dir string) (string, error) {
	if dir == "" {
		dir = "cmd/server-manager"

		if _, err := os.Stat("config.yml"); err == nil {
			dir = "."
		}
	}

	abs, err := filepath.Abs(dir)

	if err != nil {
		return "", err
	}

	if _, err := os.Stat(filepath.Join(abs, "config.yml")); err != nil {
		return "", fmt.Errorf("no config.yml in %s (run ./dev/run.ps1 -NoRun first, or pass -dir): %v", abs, err)
	}

	return abs, nil
}

func run(dir string, reset bool, out io.Writer) error {
	dir, err := resolveDir(dir)

	if err != nil {
		return err
	}

	// paths in config.yml (database, install path) are relative to the working directory of the server
	if err := os.Chdir(dir); err != nil {
		return err
	}

	conf, err := smconfig.ReadConfig("config.yml")

	if err != nil {
		return fmt.Errorf("read config.yml: %v", err)
	}

	servermanager.SetAssettoInstallPath(conf.Steam.InstallPath)

	if _, err := os.Stat(servermanager.ServerInstallPath); err != nil {
		return fmt.Errorf("assetto install path %s does not exist (run ./dev/run.ps1 -NoRun first): %v", servermanager.ServerInstallPath, err)
	}

	store, closeStore, err := openStore(conf)

	if err != nil {
		return err
	}

	defer closeStore()

	return seed(store, reset, time.Now(), out)
}

// openStore opens the store of the config. BoltDB is opened with a timeout, so a running server is reported
// instead of waiting for the database lock forever.
func openStore(conf *smconfig.Configuration) (servermanager.Store, func(), error) {
	if conf.Store.Type != "boltdb" {
		store, err := servermanager.BuildStore(&conf.Store)

		return store, func() {}, err
	}

	db, err := bbolt.Open(conf.Store.Path, 0644, &bbolt.Options{Timeout: 2 * time.Second})

	if err == bbolt.ErrTimeout || (err != nil && strings.Contains(err.Error(), "used by another process")) {
		return nil, nil, fmt.Errorf("the database %s is locked, is server-manager still running? Stop it and try again", conf.Store.Path)
	} else if err != nil {
		return nil, nil, err
	}

	store := servermanager.NewBoltStore(db)

	if err := servermanager.Migrate(store); err != nil {
		_ = db.Close()

		return nil, nil, err
	}

	return store, func() { _ = db.Close() }, nil
}

func loadServerID(store servermanager.Store) (servermanager.ServerID, error) {
	var id servermanager.ServerID

	err := store.GetMeta("server_id", &id)

	if err == servermanager.ErrValueNotSet || (err == nil && id == "") {
		// the server never ran against this database yet, it will pick up the id we store here (see initServerID)
		id = servermanager.ServerID(seedID("server").String())

		return id, store.SetMeta("server_id", id)
	}

	return id, err
}

func isSeeded(store servermanager.Store) (bool, error) {
	var m seedManifest

	err := store.GetMeta(manifestMetaKey, &m)

	if err != nil && err != servermanager.ErrValueNotSet {
		return false, err
	}

	if err == nil && m.Version > 0 {
		return true, nil
	}

	// also treat a championship of that name (e.g. from a half finished earlier run) as seeded
	if c, err := store.LoadChampionship(devCupID.String()); err == nil && c.Deleted.IsZero() {
		return true, nil
	}

	return false, nil
}

func seed(store servermanager.Store, reset bool, now time.Time, out io.Writer) error {
	if reset {
		if err := resetSeed(store, out); err != nil {
			return err
		}
	}

	seeded, err := isSeeded(store)

	if err != nil {
		return err
	}

	if seeded {
		fmt.Fprintln(out, "Dev data is already seeded, nothing to do (use -reset to recreate it).")

		return checkStandings(out, store, devCupID.String())
	}

	serverID, err := loadServerID(store)

	if err != nil {
		return err
	}

	manifest, summary, err := seedAll(store, serverID, now)

	if err != nil {
		return err
	}

	if err := store.SetMeta(manifestMetaKey, manifest); err != nil {
		return err
	}

	fmt.Fprintf(out, "Seeded %s.\n\n", summary)

	for _, id := range []string{devCupID.String(), devSprintID.String()} {
		if err := checkStandings(out, store, id); err != nil {
			return err
		}
	}

	return nil
}

// resetSeed removes everything a previous run created. Objects are found by their fixed ids, result files by the
// manifest. Deleting is the same (soft) delete the UI does.
func resetSeed(store servermanager.Store, out io.Writer) error {
	var m seedManifest

	if err := store.GetMeta(manifestMetaKey, &m); err != nil && err != servermanager.ErrValueNotSet {
		return err
	}

	removed := removeResultFiles(m.ResultFiles)

	for _, d := range seedDrivers {
		if err := store.DeleteEntrant(d.GUID); err != nil {
			return err
		}
	}

	for _, id := range []string{devCupID.String(), devSprintID.String()} {
		if c, err := store.LoadChampionship(id); err == nil && c.Deleted.IsZero() {
			if err := store.DeleteChampionship(id); err != nil {
				return err
			}
		}
	}

	for _, name := range customRaceName {
		if race, err := store.FindCustomRaceByID(seedID("customrace/" + name).String()); err == nil && race.Deleted.IsZero() {
			if err := store.DeleteCustomRace(race); err != nil {
				return err
			}
		}
	}

	if rw, err := store.LoadRaceWeekend(weekendID.String()); err == nil && rw.Deleted.IsZero() {
		if err := store.DeleteRaceWeekend(weekendID.String()); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "Removed previously seeded data (%d result files).\n", removed)

	return store.SetMeta(manifestMetaKey, &seedManifest{})
}

// checkStandings loads a championship back from the store and prints its class standings. It fails if the
// standings look broken: no points at all, or a championship with DropWorstResults where nothing is dropped.
func checkStandings(out io.Writer, store servermanager.Store, id string) error {
	c, err := store.LoadChampionship(id)

	if err != nil {
		return fmt.Errorf("load seeded championship %s: %v", id, err)
	}

	fmt.Fprintf(out, "Standings of %q (drop worst results: %d, race settings: %t, completed events: %d/%d)\n",
		c.Name, c.DropWorstResults, c.HasRaceSettings(), c.NumCompletedEvents(), len(c.Events))

	eventLabel := make(map[string]string)

	for i, e := range c.Events {
		eventLabel[e.ID.String()] = fmt.Sprintf("R%d", i+1)
	}

	totalDropped := 0

	for _, class := range c.Classes {
		standings := class.Standings(c, c.Events)
		nonZero := 0

		fmt.Fprintf(out, "  Class %s\n", class.Name)

		for pos, s := range standings {
			var dropped []string

			for eventID := range s.DroppedEvents {
				dropped = append(dropped, eventLabel[eventID])
			}

			sort.Strings(dropped)

			totalDropped += len(dropped)

			if s.Points > 0 {
				nonZero++
			}

			team := s.Car.Driver.Team

			if team == "" {
				team = "-"
			}

			fmt.Fprintf(out, "    %2d. %-16s %-20s %6.1f pts  (dropped %5.1f from: %s)\n",
				pos+1, s.Car.Driver.Name, team, s.Points, s.DroppedPoints, strings.Join(dropped, ","))
		}

		if nonZero == 0 {
			return fmt.Errorf("class %s of %s has no standings with points", class.Name, c.Name)
		}

		for _, ts := range class.TeamStandings(c, c.Events) {
			fmt.Fprintf(out, "    team %-20s %6.1f pts\n", ts.Team, ts.Points)
		}
	}

	if c.DropWorstResults > 0 && totalDropped == 0 {
		return fmt.Errorf("%s has DropWorstResults=%d but no result was dropped", c.Name, c.DropWorstResults)
	}

	fmt.Fprintln(out)

	return nil
}
