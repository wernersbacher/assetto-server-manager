package servers

import (
	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// activeEvent returns the event the process of the slot is running, if there is one.
func (s *Slot) activeEvent() smmodel.RaceEvent {
	if s.Process == nil || !s.Process.IsRunning() {
		return nil
	}

	return s.Process.Event()
}

func (s *Slot) ref() smweb.ServerRef {
	return smweb.ServerRef{ID: s.ID(), Name: s.Name}
}

// ChampionshipActiveOn returns the slot which runs an event of the championship, or a session of one of its race
// weekends. It is the lookup the championship and race weekend managers use (smchampionship.ActiveEvents), as they
// only know their own slot.
func (r *Registry) ChampionshipActiveOn(id uuid.UUID) (smweb.ServerRef, bool) {
	for _, slot := range r.slots {
		switch event := slot.activeEvent().(type) {
		case *smmodel.ActiveChampionship:
			if event.ChampionshipID == id {
				return slot.ref(), true
			}
		case *smmodel.ActiveRaceWeekend:
			if event.ChampionshipID == id {
				return slot.ref(), true
			}
		case smmodel.ActiveRaceWeekend:
			if event.ChampionshipID == id {
				return slot.ref(), true
			}
		}
	}

	return smweb.ServerRef{}, false
}

// RaceWeekendActiveOn returns the slot which runs a session of the race weekend.
func (r *Registry) RaceWeekendActiveOn(id uuid.UUID) (smweb.ServerRef, bool) {
	for _, slot := range r.slots {
		switch event := slot.activeEvent().(type) {
		case *smmodel.ActiveRaceWeekend:
			if event.RaceWeekendID == id {
				return slot.ref(), true
			}
		case smmodel.ActiveRaceWeekend:
			if event.RaceWeekendID == id {
				return slot.ref(), true
			}
		}
	}

	return smweb.ServerRef{}, false
}
