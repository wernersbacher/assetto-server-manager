package championship

import (
	"net/http"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// listAPI returns all championships in the API format.
func (ch *ChampionshipsHandler) ListAPI(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championships, err := cm.ListChampionships()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list championships")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	out := make([]*api.Championship, 0, len(championships))

	for _, championship := range championships {
		apiChampionship := api.NewChampionship(cm.env.InstallPath(), championship, ch.config.HTTP.BaseURL)
		ch.setActiveServer(apiChampionship)

		out = append(out, apiChampionship)
	}

	api.WriteJSON(w, out)
}

// viewAPI returns a single championship in the API format.
func (ch *ChampionshipsHandler) ViewAPI(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err == smstore.ErrChampionshipNotFound {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	out := api.NewChampionship(cm.env.InstallPath(), championship, ch.config.HTTP.BaseURL)
	ch.setActiveServer(out)

	api.WriteJSON(w, out)
}

// setActiveServer sets the server which runs an event of the championship, if there is one.
func (ch *ChampionshipsHandler) setActiveServer(championship *api.Championship) {
	if ch.active == nil {
		return
	}

	if server, ok := ch.active.ChampionshipActiveOn(championship.ID); ok {
		championship.ActiveServer = &api.Server{ID: string(server.ID), Name: server.Name}
	}
}

// StandingsAPI returns the driver and team standings of a championship in the API format.
func (ch *ChampionshipsHandler) StandingsAPI(w http.ResponseWriter, r *http.Request) {
	cm, ok := ch.manager(w, r)

	if !ok {
		return
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err == smstore.ErrChampionshipNotFound {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	api.WriteJSON(w, api.NewChampionshipStandings(championship))
}
