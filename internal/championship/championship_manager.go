package championship

import (
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/notification"
	smrace "github.com/wernersbacher/ACPitwall/internal/race"
	"github.com/wernersbacher/ACPitwall/internal/reminder"
	smserver "github.com/wernersbacher/ACPitwall/internal/server"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"

	"errors"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/ACPitwall/pkg/when"
)

type ChampionshipManager struct {
	*smrace.RaceManager

	config              *smconfig.Configuration
	store               smstore.Store
	process             smserver.ServerProcess
	env                 *serverenv.Env
	notificationManager notification.Dispatcher

	activeChampionship *smmodel.ActiveChampionship
	mutex              sync.Mutex

	championshipEventStartTimers    map[string]*when.Timer
	championshipEventReminderTimers reminder.Timers
}

// NewChampionshipManager creates the ChampionshipManager. The dependencies are the ones of the race manager, which it
// embeds.
func NewChampionshipManager(
	raceManager *smrace.RaceManager,
	config *smconfig.Configuration,
	store smstore.Store,
	process smserver.ServerProcess,
	env *serverenv.Env,
	notificationManager notification.Dispatcher,
) *ChampionshipManager {
	return &ChampionshipManager{
		RaceManager:         raceManager,
		config:              config,
		store:               store,
		process:             process,
		env:                 env,
		notificationManager: notificationManager,
	}
}

func (cm *ChampionshipManager) ApplyConfigAndStart(championship *smmodel.ActiveChampionship) error {
	cm.activeChampionship = championship
	err := cm.RaceManager.ApplyConfigAndStart(championship)

	if err != nil {
		return err
	}

	return nil
}

func (cm *ChampionshipManager) LoadChampionship(id string) (*smmodel.Championship, error) {
	return cm.store.LoadChampionship(id)
}

func (cm *ChampionshipManager) UpsertChampionship(c *smmodel.Championship) error {
	err := cm.store.UpsertChampionship(c)

	if err != nil {
		return err
	}

	return nil
}

func (cm *ChampionshipManager) DeleteChampionship(id string) error {
	return cm.store.DeleteChampionship(id)
}

func (cm *ChampionshipManager) ListChampionships() ([]*smmodel.Championship, error) {
	championships, err := cm.store.ListChampionships()

	if err != nil {
		return nil, err
	}

	sort.Slice(championships, func(i, j int) bool {
		return championships[i].Updated.After(championships[j].Updated)
	})

	for _, championship := range championships {
		for _, event := range championship.Events {
			if event.IsRaceWeekend() {
				event.RaceWeekend, err = cm.store.LoadRaceWeekend(event.RaceWeekendID.String())

				if err == smmodel.ErrRaceWeekendNotFound {
					continue
				} else if err != nil {
					return nil, err
				}
			}
		}
	}

	return championships, nil
}

type ChampionshipTemplateVars struct {
	*smrace.RaceTemplateVars

	DefaultPoints             smmodel.ChampionshipPoints
	DefaultClass              *smmodel.ChampionshipClass
	AvailableChampionshipTabs []smmodel.ChampionshipTab
}

func (cm *ChampionshipManager) BuildChampionshipOpts(r *http.Request) (championship *smmodel.Championship, opts *ChampionshipTemplateVars, err error) {
	raceOpts, err := cm.BuildRaceOpts(r)

	if err != nil {
		return nil, nil, err
	}

	defaultClass := smmodel.NewChampionshipClass("")
	defaultClass.ID = uuid.Nil

	opts = &ChampionshipTemplateVars{
		RaceTemplateVars:          raceOpts,
		DefaultPoints:             smmodel.DefaultChampionshipPoints,
		DefaultClass:              defaultClass,
		AvailableChampionshipTabs: smmodel.AvailableChampionshipTabs,
	}

	championshipID := chi.URLParam(r, "championshipID")

	isEditingChampionship := championshipID != ""
	opts.IsEditing = isEditingChampionship

	if isEditingChampionship {
		championship, err = cm.LoadChampionship(championshipID)

		if err != nil {
			return nil, nil, err
		}
	} else {
		championship = smmodel.NewChampionship("")
	}

	opts.Championship = championship

	return championship, opts, nil
}

func (cm *ChampionshipManager) HandleCreateChampionship(r *http.Request) (championship *smmodel.Championship, edited bool, err error) {
	if err := r.ParseForm(); err != nil {
		return nil, false, err
	}

	if err := validateChampionshipForm(r); err != nil {
		return nil, false, err
	}

	previousClasses := make(map[uuid.UUID]smmodel.ChampionshipClass)

	if championshipID := r.FormValue("Editing"); championshipID != "" {
		// championship is being edited. find the current version
		edited = true

		var err error

		championship, err = cm.LoadChampionship(championshipID)

		if err != nil {
			return nil, edited, err
		}

		for _, class := range championship.Classes {
			previousClasses[class.ID] = *class
		}

		championship.Classes = []*smmodel.ChampionshipClass{}
	} else {
		// new championship
		championship = smmodel.NewChampionship("")
	}

	championship.Name = r.FormValue("ChampionshipName")
	championship.OpenEntrants = r.FormValue("ChampionshipOpenEntrants") == "on" || r.FormValue("ChampionshipOpenEntrants") == "1"
	dropWorstResults, err := parseDropWorstResults(r.FormValue("ChampionshipDropWorstResults"))

	if err != nil {
		return nil, edited, err
	}

	championship.DropWorstResults = dropWorstResults
	championship.RaceSettings = ParseChampionshipRaceSettings(r)
	championship.PersistOpenEntrants = r.FormValue("ChampionshipPersistOpenEntrants") == "on" || r.FormValue("ChampionshipPersistOpenEntrants") == "1"
	championship.SignUpForm.Enabled = r.FormValue("Championship.SignUpForm.Enabled") == "on" || r.FormValue("Championship.SignUpForm.Enabled") == "1"
	championship.SignUpForm.AskForEmail = r.FormValue("Championship.SignUpForm.AskForEmail") == "on" || r.FormValue("Championship.SignUpForm.AskForEmail") == "1"
	championship.SignUpForm.AskForTeam = r.FormValue("Championship.SignUpForm.AskForTeam") == "on" || r.FormValue("Championship.SignUpForm.AskForTeam") == "1"
	championship.SignUpForm.HideCarChoice = !(r.FormValue("Championship.SignUpForm.HideCarChoice") == "on" || r.FormValue("Championship.SignUpForm.HideCarChoice") == "1")
	championship.SignUpForm.RequiresApproval = r.FormValue("Championship.SignUpForm.RequiresApproval") == "on" || r.FormValue("Championship.SignUpForm.RequiresApproval") == "1"

	championship.SignUpForm.ExtraFields = []string{}

	for _, question := range r.Form["Championship.SignUpForm.ExtraFields"] {
		if question == "" {
			continue
		}

		championship.SignUpForm.ExtraFields = append(championship.SignUpForm.ExtraFields, question)
	}

	championship.Info = template.HTML(r.FormValue("ChampionshipInfo"))
	championship.DefaultTab = smmodel.ChampionshipTab(r.FormValue("ChampionshipDefaultTab"))
	championship.OverridePassword = r.FormValue("OverridePassword") == "on" || r.FormValue("OverridePassword") == "1"

	championship.OGImage = r.FormValue("ChampionshipOGImage")

	championship.ReplacementPassword = r.FormValue("ReplacementPassword")

	previousNumEntrants := 0
	previousNumPoints := 0
	previousNumCars := 0

	// spectator car, the first entrant in the form
	if len(r.Form["EntryList.Name"]) > previousNumEntrants {
		entrants, err := cm.BuildEntryList(r, previousNumEntrants, 1)

		if err != nil {
			return nil, edited, err
		}

		championship.SpectatorCar = *(entrants.AsSlice()[0])

		previousNumEntrants++
		previousNumCars += smweb.FormValueAsInt(r.FormValue("NumAvailableSpectatorCars"))
		championship.SpectatorCarEnabled = smweb.FormValueAsInt(r.FormValue("Championship.SpectatorCar.Enabled")) == 1
	}

	for i := 0; i < len(r.Form["ClassName"]); i++ {
		class := smmodel.NewChampionshipClass(r.Form["ClassName"][i])

		if classID := r.Form["ClassID"][i]; classID != "" && classID != uuid.Nil.String() {
			class.ID = uuid.MustParse(classID)
		}

		numEntrantsForClass := smweb.FormValueAsInt(r.Form["EntryList.NumEntrants"][i])
		numPointsForClass := smweb.FormValueAsInt(r.Form["NumPoints"][i])
		numCarsForClass := smweb.FormValueAsInt(r.Form["NumCars"][i])

		class.AvailableCars = r.Form["Cars"][previousNumCars : previousNumCars+numCarsForClass]

		class.Entrants, err = cm.BuildEntryList(r, previousNumEntrants, numEntrantsForClass)

		if err != nil {
			return nil, edited, err
		}

		class.Points.Places = make([]int, 0)

		for i := previousNumPoints; i < previousNumPoints+numPointsForClass; i++ {
			class.Points.Places = append(class.Points.Places, smweb.FormValueAsInt(r.Form["Points.Place"][i]))
		}

		class.Points.PolePosition = smweb.FormValueAsInt(r.Form["Points.PolePosition"][i])
		class.Points.BestLap = smweb.FormValueAsInt(r.Form["Points.BestLap"][i])
		class.Points.SecondRaceMultiplier = smweb.FormValueAsFloat(r.Form["Points.SecondRaceMultiplier"][i])
		class.Points.CollisionWithDriver = smweb.FormValueAsInt(r.Form["Points.CollisionWithDriver"][i])
		class.Points.CollisionWithEnv = smweb.FormValueAsInt(r.Form["Points.CollisionWithEnv"][i])
		class.Points.CutTrack = smweb.FormValueAsInt(r.Form["Points.CutTrack"][i])

		if previousClass, ok := previousClasses[class.ID]; ok {
			// look for previous penalties and apply them back across
			class.DriverPenalties = previousClass.DriverPenalties
			class.TeamPenalties = previousClass.TeamPenalties
		}

		championship.AddClass(class)

		// ensure that linked race weekends (if any) have points set up for this class
		for _, event := range championship.Events {
			if !event.IsRaceWeekend() {
				continue
			}

			updatedRaceWeekend := false

			for _, session := range event.RaceWeekend.Sessions {
				if _, ok := session.Points[class.ID]; ok {
					continue
				}

				if session.SessionType() == serverconfig.SessionTypeRace {
					session.Points[class.ID] = &class.Points
				} else {
					session.Points[class.ID] = &smmodel.ChampionshipPoints{Places: make([]int, len(class.Points.Places))}
				}

				updatedRaceWeekend = true
			}

			if updatedRaceWeekend {
				if err := cm.store.UpsertRaceWeekend(event.RaceWeekend); err != nil {
					return nil, edited, err
				}
			}
		}

		previousNumEntrants += numEntrantsForClass
		previousNumPoints += numPointsForClass
		previousNumCars += numCarsForClass
	}

	// persist any entrants so that they can be autofilled
	if err := cm.SaveEntrantsForAutoFill(championship.AllEntrants()); err != nil {
		return nil, edited, err
	}

	// look to see if any entrants have their team points set to transfer, move them across to the team they are in now
	for _, class := range championship.Classes {
		for _, entrant := range class.Entrants {
			if !entrant.TransferTeamPoints {
				continue
			}

			logrus.Infof("Renaming team for entrant: %s (%s)", entrant.Name, entrant.GUID)

			for _, event := range championship.Events {
				if event.IsRaceWeekend() && event.RaceWeekend != nil {
					for _, session := range event.RaceWeekend.Sessions {
						if !session.Completed() {
							continue
						}

						class.AttachEntrantToResult(entrant, session.Results)
					}

					if err := cm.store.UpsertRaceWeekend(event.RaceWeekend); err != nil {
						return nil, edited, err
					}
				} else {
					for _, session := range event.Sessions {
						if session.Results == nil {
							continue
						}

						class.AttachEntrantToResult(entrant, session.Results)
					}
				}
			}
		}
	}

	// look at each entrant to see if their properties should overwrite all event properties set up in the
	// event entrylist. this is useful for globally changing skins, restrictor values etc.
	for _, class := range championship.Classes {
		for _, entrant := range class.Entrants {
			if !entrant.OverwriteAllEvents {
				continue
			}

			for _, event := range championship.Events {
				eventEntrant := event.EntryList.FindEntrantByInternalUUID(entrant.InternalUUID)

				logrus.Infof("Overwriting properties for entrant: %s (%s)", entrant.Name, entrant.GUID)

				eventEntrant.OverwriteProperties(entrant)
			}
		}
	}

	return championship, edited, cm.UpsertChampionship(championship)
}

// parseDropWorstResults parses the "drop worst N results" form value. An empty value means 0 (off).
func parseDropWorstResults(value string) (int, error) {
	value = strings.TrimSpace(value)

	if value == "" {
		return 0, nil
	}

	n, err := strconv.Atoi(value)

	if err != nil {
		return 0, fmt.Errorf("servermanager: invalid number of dropped results %q: %w", value, err)
	}

	if n < 0 {
		return 0, errors.New("servermanager: the number of dropped results must not be negative")
	}

	return n, nil
}
