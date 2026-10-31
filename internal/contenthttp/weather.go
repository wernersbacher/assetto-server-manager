package contenthttp

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
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
	// Installed holds the keys of the presets which have a folder on the server and can therefore be deleted.
	Installed map[string]bool
}

func (wh *WeatherHandler) List(w http.ResponseWriter, r *http.Request) {
	weather, err := smcontent.ListWeather(wh.env.InstallPath())

	if err != nil {
		logrus.WithError(err).Errorf("could not get weather list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	wh.ViewRenderer.MustLoadTemplate(w, r, "content/weather.html", &weatherListTemplateVars{
		Weathers:  weather,
		Installed: installedWeatherPresets(wh.env.InstallPath(), weather),
	})
}

// installedWeatherPresets returns the keys of the listed presets which exist as a folder on the server.
func installedWeatherPresets(installPath string, weather smcontent.Weather) map[string]bool {
	installed := make(map[string]bool)

	for key := range weather {
		if info, err := os.Stat(filepath.Join(installPath, "content", "weather", key)); err == nil && info.IsDir() {
			installed[key] = true
		}
	}

	return installed
}

// errWeatherNotInstalled is returned if there is no folder for a weather preset, e.g. for the built-in presets
// which are always listed.
var errWeatherNotInstalled = errors.New("contenthttp: weather preset is not installed")

// removeWeatherPreset deletes the folder of an installed weather preset. The built-in presets are always listed
// even if there are no files for them, so there is nothing to delete for them.
func removeWeatherPreset(installPath, key string) error {
	weatherPath := filepath.Join(installPath, "content", "weather")

	// the key comes from the URL, only direct sub folders of the weather folder may be removed
	if key == "" || key == "." || key == ".." || strings.ContainsAny(key, `/\`) {
		return errWeatherNotInstalled
	}

	info, err := os.Stat(filepath.Join(weatherPath, key))

	if os.IsNotExist(err) || (err == nil && !info.IsDir()) {
		return errWeatherNotInstalled
	} else if err != nil {
		return err
	}

	return os.RemoveAll(filepath.Join(weatherPath, key))
}

func (wh *WeatherHandler) Delete(w http.ResponseWriter, r *http.Request) {
	weatherKey := chi.URLParam(r, "key")

	err := removeWeatherPreset(wh.env.InstallPath(), weatherKey)

	switch {
	case err == nil:
		smweb.AddFlash(w, r, "Weather preset successfully deleted!")
	case err == errWeatherNotInstalled:
		// inform weather wasn't found
		smweb.AddErrorFlash(w, r, "Sorry, weather preset could not be deleted. Are you sure it was installed? Built-in presets cannot be deleted.")
	default:
		logrus.WithError(err).Errorf("could not remove weather files")
		smweb.AddErrorFlash(w, r, "Sorry, weather preset could not be deleted.")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}
