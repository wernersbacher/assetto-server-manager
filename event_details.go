package servermanager

import (
	"fmt"
	"net/http"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// EventDetailsHandler renders the popup with the details of a custom race, a championship event or a race weekend
// session. It needs the managers of all of them, which is why it is not part of one of their handlers.
type EventDetailsHandler struct {
	*smweb.BaseHandler

	store               smstore.Store
	championshipManager *smchampionship.ChampionshipManager
	raceWeekendManager  *RaceWeekendManager
}

func NewEventDetailsHandler(base *smweb.BaseHandler, store smstore.Store, championshipManager *smchampionship.ChampionshipManager, raceWeekendManager *RaceWeekendManager) *EventDetailsHandler {
	return &EventDetailsHandler{
		BaseHandler:         base,
		store:               store,
		championshipManager: championshipManager,
		raceWeekendManager:  raceWeekendManager,
	}
}

type eventDetailsTemplateVars struct {
	smweb.BaseTemplateVars

	EventConfig    serverconfig.CurrentRaceConfig
	EntryList      serverconfig.EntryList
	EventName      string
	IsChampionship bool
}

func (edh *EventDetailsHandler) view(w http.ResponseWriter, r *http.Request) {
	var (
		eventConfig    serverconfig.CurrentRaceConfig
		eventName      string
		entryList      serverconfig.EntryList
		isChampionship bool
	)

	if customRaceID := r.URL.Query().Get("custom-race"); customRaceID != "" {
		race, err := edh.store.FindCustomRaceByID(customRaceID)

		if err != nil {
			http.NotFound(w, r)
			return
		}

		eventConfig = race.RaceConfig
		eventName = race.Name
		entryList = race.EntryList
	} else if championshipID := r.URL.Query().Get("championshipID"); championshipID != "" {
		championship, err := edh.championshipManager.LoadChampionship(championshipID)

		if err != nil {
			http.NotFound(w, r)
			return
		}

		eventID := r.URL.Query().Get("eventID")

		event, _, err := championship.EventByID(eventID)

		if err != nil {
			http.NotFound(w, r)
			return
		}

		eventName = "Championship Event"
		eventConfig, entryList = edh.championshipManager.FinalEventConfigurationFiles(championship, event, false)

		isChampionship = true
	} else if raceWeekendID := r.URL.Query().Get("raceWeekendID"); raceWeekendID != "" {
		raceWeekend, err := edh.raceWeekendManager.LoadRaceWeekend(raceWeekendID)

		if err != nil {
			http.NotFound(w, r)
			return
		}

		sessionID := r.URL.Query().Get("sessionID")

		session, err := raceWeekend.FindSessionByID(sessionID)

		if err != nil {
			http.NotFound(w, r)
			return
		}

		eventConfig = session.RaceConfig

		if raceWeekend.HasLinkedChampionship() {
			eventConfig = raceWeekend.Championship.EffectiveRaceWeekendSessionSetup(raceWeekend.ID, eventConfig)
		}

		eventName = fmt.Sprintf("%s (%s)", session.Name(), raceWeekend.Name)
		rwe, err := getRaceWeekendEntryList(session, raceWeekend, nil, "")

		if err != nil {
			http.NotFound(w, r)
			return
		}

		entryList = rwe.AsEntryList()
		isChampionship = raceWeekend.HasLinkedChampionship()
	}

	if eventConfig.Track == "" {
		http.NotFound(w, r)
		return
	}

	edh.ViewRenderer.MustLoadPartial(w, r, "custom-race/popups/view.html", &eventDetailsTemplateVars{
		EventConfig:    eventConfig,
		EventName:      eventName,
		EntryList:      entryList,
		IsChampionship: isChampionship,
	})
}
