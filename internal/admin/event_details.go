package admin

import (
	"fmt"
	"net/http"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// EventDetailsHandler renders the popup with the details of a custom race, a championship event or a race weekend
// session. It needs the managers of all of them, which is why it is not part of one of their handlers.
type EventDetailsHandler struct {
	*smweb.BaseHandler

	slotFor SlotFor
}

func NewEventDetailsHandler(base *smweb.BaseHandler, slotFor SlotFor) *EventDetailsHandler {
	return &EventDetailsHandler{
		BaseHandler: base,
		slotFor:     slotFor,
	}
}

type eventDetailsTemplateVars struct {
	smweb.BaseTemplateVars

	EventConfig    serverconfig.CurrentRaceConfig
	EntryList      serverconfig.EntryList
	EventName      string
	IsChampionship bool
}

func (edh *EventDetailsHandler) View(w http.ResponseWriter, r *http.Request) {
	slot, ok := edh.slot(w, r)

	if !ok {
		return
	}

	var (
		eventConfig    serverconfig.CurrentRaceConfig
		eventName      string
		entryList      serverconfig.EntryList
		isChampionship bool
	)

	if customRaceID := r.URL.Query().Get("custom-race"); customRaceID != "" {
		race, err := slot.Store.FindCustomRaceByID(customRaceID)

		if err != nil {
			http.NotFound(w, r)
			return
		}

		eventConfig = race.RaceConfig
		eventName = race.Name
		entryList = race.EntryList
	} else if championshipID := r.URL.Query().Get("championshipID"); championshipID != "" {
		championship, err := slot.ChampionshipManager.LoadChampionship(championshipID)

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

		eventName = smweb.T(r, "race.form.championship_event")
		eventConfig, entryList = slot.ChampionshipManager.FinalEventConfigurationFiles(championship, event, false)

		isChampionship = true
	} else if raceWeekendID := r.URL.Query().Get("raceWeekendID"); raceWeekendID != "" {
		raceWeekend, err := slot.RaceWeekendManager.LoadRaceWeekend(raceWeekendID)

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
		entryList, err = slot.RaceWeekendManager.SessionEntryList(session, raceWeekend)

		if err != nil {
			http.NotFound(w, r)
			return
		}

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
