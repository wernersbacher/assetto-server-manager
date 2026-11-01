package contenthttp

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	"github.com/wernersbacher/ACPitwall/internal/plainname"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// SetupsHandler uploads and deletes car setups.
type SetupsHandler struct {
	env *serverenv.Env
}

func NewSetupsHandler(env *serverenv.Env) *SetupsHandler {
	return &SetupsHandler{env: env}
}

func (sh *SetupsHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if carName, err := smcontent.UploadCarSetup(sh.env.InstallPath(), r); err != nil {
		logrus.WithError(err).Errorf("Could not upload setup file")

		if carName != "" {
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.content.setup_upload_failed_car", "car", carName))
		} else {
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.content.setup_upload_failed"))
		}
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.content.setup_uploaded", "car", carName))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (sh *SetupsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	carName := chi.URLParam(r, "car")
	trackName := chi.URLParam(r, "track")
	setupName := chi.URLParam(r, "setup")

	if !plainname.Valid(carName) || !plainname.Valid(trackName) || !plainname.Valid(setupName) {
		http.NotFound(w, r)
		return
	}

	err := os.RemoveAll(filepath.Join(sh.env.InstallPath(), "setups", carName, trackName, setupName))

	if err != nil {
		logrus.WithError(err).Errorf("Could not remove setup %s/%s/%s", carName, trackName, setupName)
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.content.setup_not_deleted", "car", carName))
	} else {
		smweb.AddFlash(w, r, smweb.T(r, "flash.content.setup_deleted"))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}
