package championship

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	smrace "github.com/wernersbacher/ACPitwall/internal/race"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// ManagerFor returns the championship manager of the server slot a request is for. The resolver provides it, as the
// handlers cannot know the slots.
type ManagerFor func(r *http.Request) (*ChampionshipManager, error)

// ActiveEvents tells on which server slot a championship or race weekend currently runs. The managers belong to one
// slot each, so the resolver provides the lookup across all slots (internal/servers implements it). A championship is
// also active on the slot which runs a session of one of its race weekends.
type ActiveEvents interface {
	ChampionshipActiveOn(id uuid.UUID) (smweb.ServerRef, bool)
	RaceWeekendActiveOn(id uuid.UUID) (smweb.ServerRef, bool)
}

// ErrServerBusy is returned when an event is started on a server which already runs an event. It is the error of the
// race manager, so that all kinds of events report it alike.
var ErrServerBusy = smrace.ErrServerBusy

// ActiveElsewhereError is returned when a championship or race weekend is used on one server while it is active on
// another one.
type ActiveElsewhereError struct {
	// Subject is what is active, e.g. "championship".
	Subject string
	Server  smweb.ServerRef
}

func (e ActiveElsewhereError) Error() string {
	return fmt.Sprintf("this %s is active on server %q (%s): select that server, or stop the event there first", e.Subject, e.Server.Name, e.Server.ID)
}

// CheckNotActiveElsewhere returns an ActiveElsewhereError if the championship is active on another server than the
// one of this manager.
func (cm *ChampionshipManager) CheckNotActiveElsewhere(championshipID string, active ActiveEvents) error {
	if active == nil {
		return nil
	}

	id, err := uuid.Parse(championshipID)

	if err != nil {
		return err
	}

	if server, ok := active.ChampionshipActiveOn(id); ok && server.ID != cm.env.ServerID() {
		return ActiveElsewhereError{Subject: "championship", Server: server}
	}

	return nil
}

// CheckCanStartEvent tells if an event of the championship may be started on the server of this manager: the server
// must not run an event and the championship must not be active on another server.
func (cm *ChampionshipManager) CheckCanStartEvent(championshipID string, active ActiveEvents) error {
	if cm.process.IsRunning() {
		return ErrServerBusy
	}

	return cm.CheckNotActiveElsewhere(championshipID, active)
}

// manager looks up the manager of the request, or answers it with 404.
func (ch *ChampionshipsHandler) manager(w http.ResponseWriter, r *http.Request) (*ChampionshipManager, bool) {
	manager, err := ch.managerFor(r)

	if err != nil {
		logrus.WithError(err).Error("couldn't find the championship manager of the server")
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return nil, false
	}

	return manager, true
}

// flashEventError tells the user why an event action failed (fallback is the translated message), if it is because
// of the state of the servers.
func flashEventError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	if reason, ok := ServerStateReason(r, err); ok {
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.championship.failed_reason", "message", fallback, "reason", reason))
		return
	}

	smweb.AddErrorFlash(w, r, fallback)
}

// ServerStateReason returns the translated reason if err is because of the state of the servers (the server is busy,
// or the event is active on another server).
func ServerStateReason(r *http.Request, err error) (string, bool) {
	var elsewhere ActiveElsewhereError

	switch {
	case errors.Is(err, ErrServerBusy):
		return smweb.T(r, "flash.championship.server_busy"), true
	case errors.As(err, &elsewhere):
		switch elsewhere.Subject {
		case "championship":
			return smweb.T(r, "flash.championship.championship_active_elsewhere", "server", elsewhere.Server.Name), true
		case "race weekend":
			return smweb.T(r, "flash.championship.race_weekend_active_elsewhere", "server", elsewhere.Server.Name), true
		case "championship of this race weekend":
			return smweb.T(r, "flash.championship.linked_championship_active_elsewhere", "server", elsewhere.Server.Name), true
		}

		return err.Error(), true
	}

	return "", false
}
