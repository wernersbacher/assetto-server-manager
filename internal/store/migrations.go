package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	defaultcontent "github.com/wernersbacher/ACPitwall/fixtures/default-content"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const (
	versionMetaKey = "version"
)

// MigrationEnv carries the process wide values which migrations depend on.
type MigrationEnv struct {
	// BuildVersion is the version of the running ACPitwall.
	BuildVersion string

	// InstallPath returns the installation path of the assetto corsa server. It is a function because the path is
	// only set after the store has been opened and migrated.
	InstallPath func() string
}

func (e MigrationEnv) installPath() string {
	if e.InstallPath == nil {
		return ""
	}

	return e.InstallPath()
}

// Migrate brings the data in the store up to date with the current version of the data structures.
func Migrate(store Store, env MigrationEnv) error {
	var storeVersion int

	err := store.GetMeta(versionMetaKey, &storeVersion)

	if err != nil && err != ErrValueNotSet {
		return err
	}

	if jsonStore, ok := store.(*JSONStore); ok {
		err := separateJSONStores(jsonStore)

		if err != nil {
			return err
		}
	}

	migrations := buildMigrations(env)

	for i := storeVersion; i < CurrentMigrationVersion; i++ {
		err := migrations[i](store)

		if err != nil {
			return err
		}
	}

	return store.SetMeta(versionMetaKey, CurrentMigrationVersion)
}

type migrationFunc func(Store) error

// CurrentMigrationVersion is the number of migrations, which is the version of up to date stores.
var CurrentMigrationVersion = len(buildMigrations(MigrationEnv{}))

// buildMigrations returns all migrations in the order they run. Never reorder or remove entries, the stored version is
// the index into this list.
func buildMigrations(env MigrationEnv) []migrationFunc {
	return []migrationFunc{
		addEntrantIDToChampionships,
		func(s Store) error { return addAdminAccount(s, env.BuildVersion) },
		// migration 2 (below) is left intentionally blank. it replaces a migration which worked with deprecated data.
		func(Store) error { return nil },
		addEntrantsToChampionshipEvents,
		addIDToChampionshipClasses,
		enhanceOldChampionshipResultFiles,
		addResultScreenTimeDefault,
		// migration 8 (below) has been left intentionally blank, as it is now migration 9
		// due to it needing re-running in some environments.
		func(Store) error { return nil },
		addPitBoxDefinitionToEntrants,
		addLastSeenVersionToAccounts,
		addSleepTime1ToServerOptions,
		addPersistOpenEntrantsToChampionship,
		addThemeChoiceToAccounts,
		addRaceWeekendExamples,
		addServerNameTemplate,
		addAvailableCarsToChampionshipClass,
		func(s Store) error { return addTyresForP13c(s, env.installPath()) },
		changeNotificationTimer,
		addContentExamples,
		addServerIDToScheduledEvents,
		addLoopServerToCustomRace,
		amendChampionshipClassIDIncorrectValues,
		enableLoggingWith5LogsKept,
		convertAccountGroupToServerIDGroupMap,
		func(Store) error { return nil }, // intentionally left blank.
		convertContentManagerDescriptionToNewTemplate,
		addHasSeenIntroPopupToAccounts,
		forceLoggingWith5LogsKeptForHosted,
		showEventDetailsPopupOn,
		addCorrectCarNameToLiveTimingsData,
		addDefaultACSRGateOptionsToChampionships,
		addSplitTypeToRaceWeekends,
		fixCarDuplicationInRaceSetups,
		addRealPenaltyAppUDPPort,
		convertOpenOptionsToVisibility,
	}
}

// serverAccountOptionsMetaKey is the meta key of the server wide account options (see the accounts package).
const serverAccountOptionsMetaKey = "server-account-options"

// convertOpenOptionsToVisibility replaces the options IsOpen, PublicStats and DownloadAccess by the visibility per
// area. The old fields are no longer part of the options struct, so they are read with a struct of their own (both
// stores decode by field name and ignore what the target lacks) and dropped when the options are written back.
//
// Stores of ACPitwall before the options existed have no value but are not new: they were closed. Only a store
// without a version at all (a new installation) keeps the defaults of the areas.
func convertOpenOptionsToVisibility(s Store) error {
	logrus.Infof("Running migration: Replace the open server options by the visibility per area")

	var legacy struct {
		IsOpen         bool
		PublicStats    bool
		DownloadAccess int // 0 public, 1 login, 2 off
	}

	var options smmodel.ServerAccountOptions

	err := s.GetMeta(serverAccountOptionsMetaKey, &legacy)

	if err == ErrValueNotSet {
		var version int

		if err := s.GetMeta(versionMetaKey, &version); err == ErrValueNotSet {
			return nil // new installation
		} else if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err := s.GetMeta(serverAccountOptionsMetaKey, &options); err != nil {
		return err
	}

	options.Visibility = visibilityFromLegacy(legacy.IsOpen, legacy.PublicStats, legacy.DownloadAccess)

	return s.SetMeta(serverAccountOptionsMetaKey, &options)
}

func visibilityFromLegacy(isOpen, publicStats bool, downloadAccess int) smmodel.Visibility {
	visibility := make(smmodel.Visibility)

	if !isOpen {
		for _, area := range smmodel.AllAreas {
			visibility[area] = smmodel.LevelLoggedIn
		}

		return visibility
	}

	if !publicStats {
		visibility[smmodel.AreaStats] = smmodel.LevelLoggedIn
	}

	switch downloadAccess {
	case 1:
		visibility[smmodel.AreaDownloads] = smmodel.LevelLoggedIn
	case 2:
		visibility[smmodel.AreaDownloads] = smmodel.LevelAdmin
	}

	return visibility
}

func addEntrantIDToChampionships(rs Store) error {
	logrus.Infof("Running migration: Add Internal UUID to Championship Entrants")

	championships, err := rs.ListChampionships()

	if err != nil {
		return err
	}

	for _, c := range championships {
		for _, class := range c.Classes {
			for _, entrant := range class.Entrants {
				if entrant.InternalUUID == uuid.Nil {
					entrant.InternalUUID = uuid.New()
				}
			}
		}

		err = rs.UpsertChampionship(c)

		if err != nil {
			return err
		}
	}

	return nil
}

func addAdminAccount(rs Store, buildVersion string) error {
	serverID, err := InitServerID(rs)

	if err != nil {
		return err
	}

	accounts, err := rs.ListAccounts()

	if err != nil {
		return err
	}

	for _, account := range accounts {
		// in a multi-server scenario, don't reset the admin account password to 'servermanager'
		if account.Name == smmodel.AdminUserName {
			return nil
		}
	}

	logrus.Infof("Running migration: Add Admin Account")

	account := smmodel.NewAccount(serverID, buildVersion)
	account.Name = smmodel.AdminUserName
	account.DefaultPassword = "servermanager"
	// DeprecatedGroup must be assigned to here, since a following migration will just re-set-up Groups.
	account.DeprecatedGroup = smmodel.GroupAdmin
	account.Groups[serverID] = smmodel.GroupAdmin

	return rs.UpsertAccount(account)
}

func addEntrantsToChampionshipEvents(rs Store) error {
	logrus.Infof("Running migration: Add entrants to championship events")

	championships, err := rs.ListChampionships()

	if err != nil {
		return err
	}

	for _, c := range championships {
		for _, event := range c.Events {
			if event.EntryList == nil || len(event.EntryList) == 0 {
				event.EntryList = c.AllEntrants()
			}
		}

		err = rs.UpsertChampionship(c)

		if err != nil {
			return err
		}
	}

	return nil
}

func addIDToChampionshipClasses(rs Store) error {
	logrus.Infof("Running migration: Add class ID to championship classes")

	championships, err := rs.ListChampionships()

	if err != nil {
		return err
	}

	for _, c := range championships {
		for _, class := range c.Classes {
			if class.ID == uuid.Nil {
				class.ID = uuid.New()
			}
		}

		err = rs.UpsertChampionship(c)

		if err != nil {
			return err
		}
	}

	return nil
}

func enhanceOldChampionshipResultFiles(rs Store) error {
	logrus.Infof("Running migration: Enhance Old Championship Results Files")

	championships, err := rs.ListChampionships()

	if err != nil {
		return err
	}

	for _, c := range championships {
		for _, event := range c.Events {
			for _, session := range event.Sessions {
				c.EnhanceResults(session.Results)
			}
		}

		err = rs.UpsertChampionship(c)

		if err != nil {
			return err
		}
	}

	return nil
}

func addResultScreenTimeDefault(rs Store) error {
	logrus.Infof("Running migration: Add Result Screen Time")

	const defaultResultScreenTime = 90

	customRaces, err := rs.ListCustomRaces()

	if err != nil {
		return err
	}

	sort.Slice(customRaces, func(i, j int) bool {
		return customRaces[i].Updated.After(customRaces[j].Updated)
	})

	for _, race := range customRaces {
		race.RaceConfig.ResultScreenTime = defaultResultScreenTime

		err := rs.UpsertCustomRace(race)

		if err != nil {
			return err
		}
	}

	championships, err := rs.ListChampionships()

	if err != nil {
		return err
	}

	sort.Slice(championships, func(i, j int) bool {
		return championships[i].Updated.After(championships[j].Updated)
	})

	for _, champ := range championships {
		for _, event := range champ.Events {
			event.RaceSetup.ResultScreenTime = defaultResultScreenTime
		}

		err := rs.UpsertChampionship(champ)

		if err != nil {
			return err
		}
	}

	return nil
}

func addPitBoxDefinitionToEntrants(rs Store) error {
	logrus.Infof("Running migration: Add Pit Box Definition To Entrants")

	customRaces, err := rs.ListCustomRaces()

	if err != nil {
		return err
	}

	sort.Slice(customRaces, func(i, j int) bool {
		return customRaces[i].Updated.After(customRaces[j].Updated)
	})

	for _, customRace := range customRaces {
		for i, entrant := range customRace.EntryList.AsSlice() {
			entrant.PitBox = i
		}

		err := rs.UpsertCustomRace(customRace)

		if err != nil {
			return err
		}
	}

	championships, err := rs.ListChampionships()

	if err != nil {
		return err
	}

	sort.Slice(championships, func(i, j int) bool {
		return championships[i].Updated.After(championships[j].Updated)
	})

	for _, championship := range championships {
		for _, event := range championship.Events {
			event.SetChampionship(championship)

			for i, entrant := range event.EntryList.AsSlice() {
				entrant.PitBox = i
			}
		}

		err := rs.UpsertChampionship(championship)

		if err != nil {
			return err
		}
	}

	return nil
}

func moveStoreFiles(oldPath string, newPath string) error {
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	logrus.WithField("from", oldPath).WithField("to", newPath).Infof("Migrating JSON private store to shared store")

	return os.Rename(oldPath, newPath)
}

func separateJSONStores(rs *JSONStore) error {
	if rs.base != rs.shared {
		err := os.MkdirAll(rs.shared, 0755)

		if err != nil {
			return err
		}

		err = moveStoreFiles(filepath.Join(rs.base, championshipsDir), filepath.Join(rs.shared, championshipsDir))

		if err != nil {
			return err
		}

		err = moveStoreFiles(filepath.Join(rs.base, customRacesDir), filepath.Join(rs.shared, customRacesDir))

		if err != nil {
			return err
		}

		err = moveStoreFiles(filepath.Join(rs.base, entrantsFile), filepath.Join(rs.shared, entrantsFile))

		if err != nil {
			return err
		}
	}

	return nil
}

const lastReleaseVersionPreChangelogShowUpdate = "v1.3.4"

func addLastSeenVersionToAccounts(s Store) error {
	logrus.Infof("Running migration: Add Last Seen Version to Accounts")
	accounts, err := s.ListAccounts()

	if err != nil {
		return err
	}

	for _, account := range accounts {
		account.LastSeenVersion = lastReleaseVersionPreChangelogShowUpdate

		err := s.UpsertAccount(account)

		if err != nil {
			return err
		}
	}

	return nil
}

func addSleepTime1ToServerOptions(s Store) error {
	logrus.Infof("Running migration: Set Server Options Sleep Time to 1")
	opts, err := s.LoadServerOptions()

	if err != nil {
		return err
	}

	opts.SleepTime = 1

	return s.UpsertServerOptions(opts)
}

func addPersistOpenEntrantsToChampionship(s Store) error {
	logrus.Infof("Running migration: enable 'Persist Open Entrants' in Championships")

	championships, err := s.ListChampionships()

	if err != nil {
		return err
	}

	sort.Slice(championships, func(i, j int) bool {
		return championships[i].Updated.Before(championships[j].Updated)
	})

	for _, champ := range championships {
		if champ.OpenEntrants {
			champ.PersistOpenEntrants = true
		}

		err := s.UpsertChampionship(champ)

		if err != nil {
			return err
		}
	}

	return nil
}

func addThemeChoiceToAccounts(s Store) error {
	logrus.Infof("Running migration: Add Theme Choice to Accounts")

	accounts, err := s.ListAccounts()

	if err != nil {
		return err
	}

	for _, account := range accounts {
		account.Theme = smmodel.ThemeDefault

		if err := s.UpsertAccount(account); err != nil {
			return err
		}
	}

	return nil
}

func addRaceWeekendExamples(s Store) error {
	logrus.Infof("Running migration: Add Race Weekend examples")

	var raceWeekend *smmodel.RaceWeekend

	err := json.Unmarshal(defaultcontent.RaceWeekendF12004spa, &raceWeekend)

	if err != nil {
		return err
	}

	return s.UpsertRaceWeekend(raceWeekend)
}

func addServerNameTemplate(s Store) error {
	logrus.Infof("Running migration: Add Server Name Template")

	opts, err := s.LoadServerOptions()

	if err != nil {
		return err
	}

	opts.ServerNameTemplate = serverconfig.DefaultServerNameTemplate

	return s.UpsertServerOptions(opts)
}

func changeNotificationTimer(s Store) error {
	logrus.Infof("Running migration: Change Notification Timer")

	opts, err := s.LoadServerOptions()

	if err != nil {
		return err
	}

	opts.NotificationReminderTimers = strconv.Itoa(opts.NotificationReminderTimer)

	if opts.NotificationReminderTimers == "0" {
		opts.NotificationReminderTimers = ""
	}

	return s.UpsertServerOptions(opts)
}

func addAvailableCarsToChampionshipClass(s Store) error {
	logrus.Infof("Running migration: Add Available Cars to Championship Class")

	championships, err := s.ListChampionships()

	if err != nil {
		return err
	}

	sort.Slice(championships, func(i, j int) bool {
		return championships[i].Updated.Before(championships[j].Updated)
	})

	for _, champ := range championships {
		for _, class := range champ.Classes {
			cars := make(map[string]bool)

			for _, e := range class.Entrants {
				cars[e.Model] = true
			}

			for car := range cars {
				class.AvailableCars = append(class.AvailableCars, car)
			}
		}

		err := s.UpsertChampionship(champ)

		if err != nil {
			return err
		}
	}

	return nil
}

func addTyresForP13c(s Store, installPath string) error {
	logrus.Debugf("Running migration: add tyres for IER P13c")

	tyres := make(map[string]string)

	for _, tyre := range smcontent.IERP13cTyres {
		tyres[tyre] = tyre
	}

	return smcontent.AddTyresToModTyres(installPath, smcontent.IERP13c, tyres)
}

func addContentExamples(s Store) error {
	logrus.Infof("Running migration: Add Content examples")

	championships, err := s.ListChampionships()

	if err != nil {
		return err
	}

	if len(championships) == 0 {
		var mx5Championship *smmodel.Championship

		if err := json.Unmarshal(defaultcontent.ChampionshipMX5CrashCourse, &mx5Championship); err != nil {
			return err
		}

		if err := s.UpsertChampionship(mx5Championship); err != nil {
			return err
		}

		var multiclassChampionship *smmodel.Championship

		if err := json.Unmarshal(defaultcontent.ChampionshipMulticlassEndurance, &multiclassChampionship); err != nil {
			return err
		}

		if err := s.UpsertChampionship(multiclassChampionship); err != nil {
			return err
		}
	}

	customRaces, err := s.ListCustomRaces()

	if err != nil {
		return err
	}

	if len(customRaces) == 0 {
		var f2004RaceSpa *smmodel.CustomRace

		if err := json.Unmarshal(defaultcontent.CustomRaceF2004Spa, &f2004RaceSpa); err != nil {
			return err
		}

		if err := s.UpsertCustomRace(f2004RaceSpa); err != nil {
			return err
		}

		var bmw235iRace *smmodel.CustomRace

		if err := json.Unmarshal(defaultcontent.CustomRaceBMWZandvoort, &bmw235iRace); err != nil {
			return err
		}

		if err := s.UpsertCustomRace(bmw235iRace); err != nil {
			return err
		}
	}

	return nil
}

func addServerIDToScheduledEvents(s Store) error {
	logrus.Infof("Running migration: Add Server ID to Scheduled Events")

	serverID, err := InitServerID(s)

	if err != nil {
		return err
	}

	customRaces, err := s.ListCustomRaces()

	if err != nil {
		return err
	}

	sort.Slice(customRaces, func(i, j int) bool {
		return customRaces[i].Updated.Before(customRaces[j].Updated)
	})

	for _, customRace := range customRaces {
		if customRace.Scheduled.IsZero() {
			continue
		}

		customRace.ScheduledServerID = serverID

		err := s.UpsertCustomRace(customRace)

		if err != nil {
			return err
		}
	}

	championships, err := s.ListChampionships()

	if err != nil {
		return err
	}

	sort.Slice(championships, func(i, j int) bool {
		return championships[i].Updated.Before(championships[j].Updated)
	})

	for _, championship := range championships {
		for _, event := range championship.Events {
			if event.Scheduled.IsZero() {
				continue
			}

			event.ScheduledServerID = serverID
		}

		err := s.UpsertChampionship(championship)

		if err != nil {
			return err
		}
	}

	raceWeekends, err := s.ListRaceWeekends()

	if err != nil {
		return err
	}

	sort.Slice(raceWeekends, func(i, j int) bool {
		return raceWeekends[i].Updated.Before(raceWeekends[j].Updated)
	})

	for _, raceWeekend := range raceWeekends {
		for _, session := range raceWeekend.Sessions {
			if session.ScheduledTime.IsZero() {
				continue
			}

			session.ScheduledServerID = serverID
		}

		err := s.UpsertRaceWeekend(raceWeekend)

		if err != nil {
			return err
		}
	}

	return nil
}

func addLoopServerToCustomRace(s Store) error {
	logrus.Infof("Running migration: Add Loop Per Server to Custom Race")

	serverID, err := InitServerID(s)

	if err != nil {
		return err
	}

	customRaces, err := s.ListCustomRaces()

	if err != nil {
		return err
	}

	for _, customRace := range customRaces {
		if customRace.Loop {
			customRace.LoopServer = make(map[smmodel.ServerID]bool)

			customRace.LoopServer[serverID] = true

			err := s.UpsertCustomRace(customRace)

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func amendChampionshipClassIDIncorrectValues(s Store) error {
	logrus.Infof("Running migration: Correcting Multiclass Championship incorrect ClassIDs (if any)")

	championships, err := s.ListChampionships()

	if err != nil {
		return err
	}

	for _, championship := range championships {
		if !championship.IsMultiClass() {
			continue
		}

		foundClasses := make(map[uuid.UUID]bool)

		for _, class := range championship.Classes {
			if _, ok := foundClasses[class.ID]; ok {
				logrus.Infof("Duplicated class id: %s in Championship: %s", class.ID, championship.ID)

				class.ID = uuid.New()

				for _, event := range championship.Events {
					if event.IsRaceWeekend() {
						rw, err := s.LoadRaceWeekend(event.RaceWeekendID.String())

						if err != nil {
							continue
						}

						for _, session := range rw.Sessions {

							if session.SessionType() == serverconfig.SessionTypeRace {
								session.Points[class.ID] = &class.Points
							} else {
								var points []int

								for range class.Points.Places {
									points = append(points, 0)
								}

								session.Points[class.ID] = &smmodel.ChampionshipPoints{
									Places:               points,
									BestLap:              0,
									PolePosition:         0,
									CollisionWithDriver:  0,
									CollisionWithEnv:     0,
									CutTrack:             0,
									SecondRaceMultiplier: 0,
								}
							}

							if !session.Completed() {
								continue
							}

							for _, car := range session.Results.Cars {
								class, err := championship.FindClassForCarModel(car.Model)

								if err != nil {
									continue
								}

								car.Driver.ClassID = class.ID
							}

							for _, result := range session.Results.Result {
								class, err := championship.FindClassForCarModel(result.CarModel)

								if err != nil {
									continue
								}

								result.ClassID = class.ID
							}

							for _, lap := range session.Results.Laps {
								class, err := championship.FindClassForCarModel(lap.CarModel)

								if err != nil {
									continue
								}

								lap.ClassID = class.ID
							}
						}

						if err := s.UpsertRaceWeekend(rw); err != nil {
							return err
						}
					} else {
						for _, session := range event.Sessions {
							if !session.Completed() {
								continue
							}

							for _, car := range session.Results.Cars {
								class, err := championship.FindClassForCarModel(car.Model)

								if err != nil {
									continue
								}

								car.Driver.ClassID = class.ID
							}

							for _, result := range session.Results.Result {
								class, err := championship.FindClassForCarModel(result.CarModel)

								if err != nil {
									continue
								}

								result.ClassID = class.ID
							}

							for _, lap := range session.Results.Laps {
								class, err := championship.FindClassForCarModel(lap.CarModel)

								if err != nil {
									continue
								}

								lap.ClassID = class.ID
							}
						}
					}
				}
			}

			foundClasses[class.ID] = true
		}

		if err := s.UpsertChampionship(championship); err != nil {
			return err
		}
	}

	return nil
}

func enableLoggingWith5LogsKept(s Store) error {
	logrus.Infof("Running migration: Enable AC Server Logging")

	opts, err := s.LoadServerOptions()

	if err != nil {
		return err
	}

	opts.LogACServerOutputToFile = true
	opts.NumberOfACServerLogsToKeep = 5

	return s.UpsertServerOptions(opts)
}

func convertAccountGroupToServerIDGroupMap(s Store) error {
	logrus.Infof("Running migration: Convert Account Group to Server ID Group Map")

	serverID, err := InitServerID(s)

	if err != nil {
		return err
	}

	accounts, err := s.ListAccounts()

	if err != nil {
		return err
	}

	for _, account := range accounts {
		if account.Groups != nil {
			if _, ok := account.Groups[serverID]; ok {
				continue
			}
		}

		if account.DeprecatedGroup == "" {
			continue
		}

		account.Groups = make(map[smmodel.ServerID]smmodel.Group)
		account.Groups[serverID] = account.DeprecatedGroup

		if err := s.UpsertAccount(account); err != nil {
			return err
		}
	}

	return nil
}

func convertContentManagerDescriptionToNewTemplate(s Store) error {
	logrus.Infof("Running migration: Convert Content Manager Description to New Template")

	serverOpts, err := s.LoadServerOptions()

	if err != nil {
		return err
	}

	if !strings.Contains(serverOpts.ContentManagerWelcomeMessage, "{{") {
		// assume that no placeholders have been added.
		serverOpts.ContentManagerWelcomeMessage += serverconfig.DefaultContentManagerDescription
	}

	return s.UpsertServerOptions(serverOpts)
}

// addHasSeenIntroPopupToAccounts is a no-op: it only applied to the removed hosted mode. It stays in the list because the stored migration version is the index.
func addHasSeenIntroPopupToAccounts(s Store) error {
	return nil
}

// forceLoggingWith5LogsKeptForHosted is a no-op: it only applied to the removed hosted mode. It stays in the list because the stored migration version is the index.
func forceLoggingWith5LogsKeptForHosted(s Store) error {
	return nil
}

func showEventDetailsPopupOn(s Store) error {
	logrus.Infof("Running migration: Enable Event Details Popup")

	opts, err := s.LoadServerOptions()

	if err != nil {
		return err
	}

	opts.ShowEventDetailsPopup = true

	return s.UpsertServerOptions(opts)
}

func addCorrectCarNameToLiveTimingsData(s Store) error {
	timings, err := s.LoadLiveTimingsData()

	if err != nil || timings == nil {
		return nil
	}

	logrus.Infof("Running migration: Correct Live Timings Car Names")

	for _, driver := range timings.Drivers {
		for key, car := range driver.Cars {
			car.CarName = smcontent.PrettifyName(key, true)
		}
	}

	if err := s.UpsertLiveTimingsData(timings); err != nil {
		return err
	}

	return nil
}

// addDefaultACSRGateOptionsToChampionships is a no-op: the ACSR integration has been removed. It stays in the list because the stored migration version is the index.
func addDefaultACSRGateOptionsToChampionships(s Store) error {
	return nil
}

func addSplitTypeToRaceWeekends(s Store) error {
	logrus.Infof("Running migration: Add Split Type to Race Weekends")

	raceWeekends, err := s.ListRaceWeekends()

	if err != nil {
		return err
	}

	for _, raceWeekend := range raceWeekends {
		for _, childSessionFilters := range raceWeekend.Filters {
			for _, filter := range childSessionFilters {
				if filter.ManualDriverSelection {
					filter.SplitType = smmodel.SplitTypeManualDriverSelection
				} else {
					filter.SplitType = smmodel.SplitTypeNumeric
				}
			}
		}

		if err := s.UpsertRaceWeekend(raceWeekend); err != nil {
			return err
		}
	}

	championships, err := s.ListChampionships()

	if err != nil {
		return err
	}

	for _, championship := range championships {
		for _, event := range championship.Events {
			if event.IsRaceWeekend() {
				raceWeekend, err := s.LoadRaceWeekend(event.RaceWeekendID.String())

				if err != nil {
					return err
				}

				for _, childSessionFilters := range raceWeekend.Filters {
					for _, filter := range childSessionFilters {
						if filter.ManualDriverSelection {
							filter.SplitType = smmodel.SplitTypeManualDriverSelection
						} else {
							filter.SplitType = smmodel.SplitTypeNumeric
						}
					}
				}

				if err := s.UpsertRaceWeekend(raceWeekend); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func fixCarDuplicationInRaceSetups(s Store) error {
	uniq := func(s []string) []string {
		filtered := make(map[string]bool)

		for _, x := range s {
			filtered[x] = true
		}

		var out []string

		for x := range filtered {
			out = append(out, x)
		}

		return out
	}

	fix := func(cars string) string {
		return strings.Join(uniq(strings.Split(cars, ";")), ";")
	}

	logrus.Infof("Running migration: Fix duplicate cars in race setups")

	customRaces, err := s.ListCustomRaces()

	if err != nil {
		return err
	}

	sort.Slice(customRaces, func(i, j int) bool {
		return customRaces[i].Updated.Before(customRaces[j].Updated)
	})

	for _, race := range customRaces {
		race.RaceConfig.Cars = fix(race.RaceConfig.Cars)

		if err := s.UpsertCustomRace(race); err != nil {
			return err
		}
	}

	championships, err := s.ListChampionships()

	if err != nil {
		return err
	}

	sort.Slice(championships, func(i, j int) bool {
		return championships[i].Updated.Before(championships[j].Updated)
	})

	for _, championship := range championships {
		for _, event := range championship.Events {
			if event.IsRaceWeekend() {
				continue
			}

			event.RaceSetup.Cars = fix(event.RaceSetup.Cars)
		}

		if err := s.UpsertChampionship(championship); err != nil {
			return err
		}
	}

	raceWeekends, err := s.ListRaceWeekends()

	if err != nil {
		return err
	}

	sort.Slice(raceWeekends, func(i, j int) bool {
		return raceWeekends[i].Updated.Before(raceWeekends[j].Updated)
	})

	for _, raceWeekend := range raceWeekends {
		for _, session := range raceWeekend.Sessions {
			session.RaceConfig.Cars = fix(session.RaceConfig.Cars)
		}

		if err := s.UpsertRaceWeekend(raceWeekend); err != nil {
			return err
		}
	}

	return nil
}

func addRealPenaltyAppUDPPort(s Store) error {
	logrus.Infof("Running migration: Add Real Penalty App UDP Port")

	rpOpts, err := s.LoadRealPenaltyOptions()

	if err != nil {
		return err
	}

	rpOpts.RealPenaltyAppConfig.General.AppUDPPort = rpOpts.RealPenaltyAppConfig.General.AppTCPPort

	return s.UpsertRealPenaltyOptions(rpOpts)
}
