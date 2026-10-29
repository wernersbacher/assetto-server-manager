package servermanager

import (
	"net/http"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/api"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

// listAPI returns all championships in the API format.
func (ch *ChampionshipsHandler) listAPI(w http.ResponseWriter, r *http.Request) {
	championships, err := ch.championshipManager.ListChampionships()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't list championships")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	out := make([]*api.Championship, 0, len(championships))

	for _, championship := range championships {
		out = append(out, api.NewChampionship(ServerInstallPath, championship, ch.config.HTTP.BaseURL))
	}

	api.WriteJSON(w, out)
}

// viewAPI returns a single championship in the API format.
func (ch *ChampionshipsHandler) viewAPI(w http.ResponseWriter, r *http.Request) {
	championship, err := ch.championshipManager.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err == smstore.ErrChampionshipNotFound {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("couldn't load championship")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	api.WriteJSON(w, api.NewChampionship(ServerInstallPath, championship, ch.config.HTTP.BaseURL))
}
