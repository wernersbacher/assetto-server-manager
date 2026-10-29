package servermanager

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	"github.com/wernersbacher/assetto-server-manager/internal/plainname"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

func carSetupsUploadHandler(w http.ResponseWriter, r *http.Request) {
	if carName, err := smcontent.UploadCarSetup(ServerInstallPath, r); err != nil {
		logrus.WithError(err).Errorf("Could not upload setup file")

		if carName != "" {
			smweb.AddErrorFlash(w, r, fmt.Sprintf("Unable to upload setup file for %s", carName))
		} else {
			smweb.AddErrorFlash(w, r, "Unable to upload setup file")
		}
	} else {
		smweb.AddFlash(w, r, fmt.Sprintf("The setup file for %s was uploaded successfully!", carName))
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func carSetupDeleteHandler(w http.ResponseWriter, r *http.Request) {
	carName := chi.URLParam(r, "car")
	trackName := chi.URLParam(r, "track")
	setupName := chi.URLParam(r, "setup")

	if !plainname.Valid(carName) || !plainname.Valid(trackName) || !plainname.Valid(setupName) {
		http.NotFound(w, r)
		return
	}

	err := os.RemoveAll(filepath.Join(ServerInstallPath, "setups", carName, trackName, setupName))

	if err != nil {
		logrus.WithError(err).Errorf("Could not remove setup %s/%s/%s", carName, trackName, setupName)
		smweb.AddErrorFlash(w, r, "Couldn't delete setup for "+carName)
	} else {
		smweb.AddFlash(w, r, "Setup successfully deleted!")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}
