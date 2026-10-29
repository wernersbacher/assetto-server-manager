package contenthttp

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// WeatherHandler serves the weather presets.
type WeatherHandler struct {
	*smweb.BaseHandler

	env *serverenv.Env
}

func NewWeatherHandler(baseHandler *smweb.BaseHandler, env *serverenv.Env) *WeatherHandler {
	return &WeatherHandler{
		BaseHandler: baseHandler,
		env:         env,
	}
}

type weatherListTemplateVars struct {
	smweb.BaseTemplateVars

	Weathers smcontent.Weather
}

func (wh *WeatherHandler) List(w http.ResponseWriter, r *http.Request) {
	weather, err := smcontent.ListWeather(wh.env.InstallPath())

	if err != nil {
		logrus.WithError(err).Errorf("could not get weather list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	wh.ViewRenderer.MustLoadTemplate(w, r, "content/weather.html", &weatherListTemplateVars{
		Weathers: weather,
	})
}

func (wh *WeatherHandler) Delete(w http.ResponseWriter, r *http.Request) {
	weatherKey := chi.URLParam(r, "key")
	weatherPath := filepath.Join(wh.env.InstallPath(), "content", "weather")

	existingWeather, err := smcontent.ListWeather(wh.env.InstallPath())

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
