package raceweekend

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// ManagerFor returns the race weekend manager of the server slot a request is for. The resolver provides it, as the
// handlers cannot know the slots.
type ManagerFor func(r *http.Request) (*RaceWeekendManager, error)

// CheckNotActiveElsewhere returns an smchampionship.ActiveElsewhereError if the race weekend, or the championship it
// is linked to, is active on another server than the one of this manager.
func (rwm *RaceWeekendManager) CheckNotActiveElsewhere(raceWeekendID string, active smchampionship.ActiveEvents) error {
	if active == nil {
		return nil
	}

	id, err := uuid.Parse(raceWeekendID)

	if err != nil {
		return err
	}

	if server, ok := active.RaceWeekendActiveOn(id); ok && server.ID != rwm.env.ServerID() {
		return smchampionship.ActiveElsewhereError{Subject: "race weekend", Server: server}
	}

	raceWeekend, err := rwm.store.LoadRaceWeekend(raceWeekendID)

	if err != nil {
		return err
	}

	if raceWeekend.HasLinkedChampionship() {
		if server, ok := active.ChampionshipActiveOn(raceWeekend.ChampionshipID); ok && server.ID != rwm.env.ServerID() {
			return smchampionship.ActiveElsewhereError{Subject: "championship of this race weekend", Server: server}
		}
	}

	return nil
}

// CheckCanStartSession tells if a session of the race weekend may be started on the server of this manager: the
// server must not run an event and the race weekend must not be active on another server.
func (rwm *RaceWeekendManager) CheckCanStartSession(raceWeekendID string, active smchampionship.ActiveEvents) error {
	if rwm.process.IsRunning() {
		return smchampionship.ErrServerBusy
	}

	return rwm.CheckNotActiveElsewhere(raceWeekendID, active)
}

// manager looks up the manager of the request, or answers it with 404.
func (rwh *RaceWeekendHandler) manager(w http.ResponseWriter, r *http.Request) (*RaceWeekendManager, bool) {
	manager, err := rwh.managerFor(r)

	if err != nil {
		logrus.WithError(err).Error("couldn't find the race weekend manager of the server")
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return nil, false
	}

	return manager, true
}

// flashSessionError tells the user why a session action failed, if it is because of the state of the servers.
func flashSessionError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	var elsewhere smchampionship.ActiveElsewhereError

	if errors.Is(err, smchampionship.ErrServerBusy) || errors.As(err, &elsewhere) {
		smweb.AddErrorFlash(w, r, fallback+": "+err.Error())
		return
	}

	smweb.AddErrorFlash(w, r, fallback)
}
