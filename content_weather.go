package servermanager

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

type WeatherHandler struct {
	*smweb.BaseHandler
}

func NewWeatherHandler(baseHandler *smweb.BaseHandler) *WeatherHandler {
	return &WeatherHandler{
		BaseHandler: baseHandler,
	}
}

type weatherListTemplateVars struct {
	smweb.BaseTemplateVars

	Weathers smcontent.Weather
}

func (wh *WeatherHandler) list(w http.ResponseWriter, r *http.Request) {
	weather, err := smcontent.ListWeather(ServerInstallPath)

	if err != nil {
		logrus.WithError(err).Errorf("could not get weather list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	wh.ViewRenderer.MustLoadTemplate(w, r, "content/weather.html", &weatherListTemplateVars{
		Weathers: weather,
	})
}

func (wh *WeatherHandler) delete(w http.ResponseWriter, r *http.Request) {
	weatherKey := chi.URLParam(r, "key")
	weatherPath := filepath.Join(ServerInstallPath, "content", "weather")

	existingWeather, err := smcontent.ListWeather(ServerInstallPath)

	if err != nil {
		logrus.WithError(err).Errorf("could not get weather list")
		smweb.AddErrorFlash(w, r, "Couldn't get weather list")
		http.Redirect(w, r, r.Referer(), http.StatusFound)
		return
	}

	var found bool

	for key := range existingWeather {
		if weatherKey == key {
			// Delete car
			found = true

			err := os.RemoveAll(filepath.Join(weatherPath, weatherKey))

			if err != nil {
				found = false
				logrus.WithError(err).Errorf("could not remove weather files")
			}

			delete(existingWeather, key)
			break
		}
	}

	if found {
		// confirm deletion
		smweb.AddFlash(w, r, "Weather preset successfully deleted!")
	} else {
		// inform weather wasn't found
		smweb.AddErrorFlash(w, r, "Sorry, weather preset could not be deleted. Are you sure it was installed?")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}
