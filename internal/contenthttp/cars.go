package contenthttp

import (
	"github.com/wernersbacher/assetto-server-manager/internal/plainname"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"

	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/blevesearch/bleve"
	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

// CarsHandler serves the car list, the car details, the car metadata and the skins.
type CarsHandler struct {
	*smweb.BaseHandler

	carManager   *smcontent.CarManager
	trackManager *smcontent.TrackManager
	env          *serverenv.Env
	records      *Records
}

func NewCarsHandler(baseHandler *smweb.BaseHandler, carManager *smcontent.CarManager, trackManager *smcontent.TrackManager, env *serverenv.Env, records *Records) *CarsHandler {
	return &CarsHandler{
		BaseHandler:  baseHandler,
		carManager:   carManager,
		trackManager: trackManager,
		env:          env,
		records:      records,
	}
}

type carListTemplateVars struct {
	smweb.BaseTemplateVars

	Results     *bleve.SearchResult
	Cars        smcontent.Cars
	Query       string
	CurrentPage int
	NumPages    int
	PageSize    int
}

func (ch *CarsHandler) List(w http.ResponseWriter, r *http.Request) {
	searchTerm := r.URL.Query().Get("q")
	page := smweb.FormValueAsInt(r.URL.Query().Get("page"))
	results, cars, err := ch.carManager.Search(r.Context(), searchTerm, page*smcontent.SearchPageSize, smcontent.SearchPageSize)

	if err != nil {
		logrus.WithError(err).Error("Could not perform search")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	numPages := int(math.Ceil(float64(results.Total) / float64(smcontent.SearchPageSize)))

	ch.ViewRenderer.MustLoadTemplate(w, r, "content/cars.html", &carListTemplateVars{
		Results:     results,
		Cars:        cars,
		Query:       searchTerm,
		CurrentPage: page,
		NumPages:    numPages,
		PageSize:    smcontent.SearchPageSize,
	})
}

type carSearchResult struct {
	CarName string `json:"CarName"`
	CarID   string `json:"CarID"`
	Class   string `json:"Class"`
	// Tags    []string `json:"Tags"`
}

func (ch *CarsHandler) SearchJSON(w http.ResponseWriter, r *http.Request) {
	searchTerm := r.URL.Query().Get("q")

	_, cars, err := ch.carManager.Search(r.Context(), searchTerm, 0, 100000)

	if err != nil {
		logrus.WithError(err).Error("Could not perform search")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var searchResults []carSearchResult

	for _, car := range cars {
		var class string

		if car.IsPaidDLC() {
			class = "bg-dlc"
		}

		if car.IsMod() {
			class = "bg-mod"
		}

		searchResults = append(searchResults, carSearchResult{
			CarName: car.Details.Name,
			CarID:   car.Name,
			Class:   class,
			// Tags:    car.Details.Tags,
		})
	}

	enc := json.NewEncoder(w)
	if ch.env.Debug {
		enc.SetIndent("", "    ")
	}
	_ = enc.Encode(searchResults)
}

func (ch *CarsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	carName := chi.URLParam(r, "name")
	err := ch.carManager.DeleteCar(carName)

	if err != nil {
		logrus.WithError(err).Errorf("Could not delete car: %s", carName)
		smweb.AddErrorFlash(w, r, "couldn't get car list")
		http.Redirect(w, r, r.Referer(), http.StatusFound)
		return
	}

	smweb.AddFlash(w, r, fmt.Sprintf("Car %s successfully deleted!", carName))
	http.Redirect(w, r, "/cars", http.StatusFound)
}

const defaultSkinURL = "/static/img/no-preview-car.png"

// CarSkinURL returns the URL of the preview of a car skin, or of a placeholder if the skin has no preview.
func CarSkinURL(installPath, car, skin string) string {
	skinPath := filepath.Join("content", "cars", car, "skins", url.PathEscape(skin), "preview.jpg")

	// look to see if the car preview image exists
	_, err := os.Stat(filepath.Join(installPath, "content", "cars", car, "skins", skin, "preview.jpg"))

	if err != nil {
		return defaultSkinURL
	}

	return "/" + filepath.ToSlash(skinPath)
}

func (ch *CarsHandler) View(w http.ResponseWriter, r *http.Request) {
	carName := chi.URLParam(r, "car_id")

	if !plainname.Valid(carName) {
		http.NotFound(w, r)
		return
	}

	templateParams, err := ch.loadCarDetailsForTemplate(carName, r)

	if os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("Could not load car details for: %s", carName)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.ViewRenderer.MustLoadTemplate(w, r, "content/car-details.html", templateParams)
}

func (ch *CarsHandler) Tags(w http.ResponseWriter, r *http.Request) {
	car := chi.URLParam(r, "name")

	if !plainname.Valid(car) {
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodPost {
		tag := r.FormValue("new-tag")
		err := ch.carManager.AddTag(car, tag)

		if err == nil {
			smweb.AddFlash(w, r, fmt.Sprintf("Successfully added the tag: %s", tag))
		} else {
			smweb.AddFlash(w, r, "Could not add tag")
		}
	} else {
		tag := r.URL.Query().Get("delete")
		err := ch.carManager.DelTag(car, tag)

		if err == nil {
			smweb.AddFlash(w, r, fmt.Sprintf("Successfully deleted the tag: %s", tag))
		} else {
			smweb.AddFlash(w, r, "Could not delete tag")
		}
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *CarsHandler) SaveMetadata(w http.ResponseWriter, r *http.Request) {
	car := chi.URLParam(r, "name")

	if !plainname.Valid(car) {
		http.NotFound(w, r)
		return
	}

	if err := ch.carManager.UpdateCarMetadata(car, r); err != nil {
		logrus.WithError(err).Errorf("Could not update car metadata for %s", car)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, "Car metadata updated successfully!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *CarsHandler) UploadSkin(w http.ResponseWriter, r *http.Request) {
	if !plainname.Valid(chi.URLParam(r, "name")) {
		http.NotFound(w, r)
		return
	}

	err := r.ParseMultipartForm(32 << 20)

	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	car := chi.URLParam(r, "name")

	err = ch.carManager.UploadSkin(car, r.MultipartForm.File)

	if err != nil {
		logrus.WithError(err).Errorf("could not upload car skin for %s", car)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, "Car skin uploaded successfully!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *CarsHandler) DeleteSkin(w http.ResponseWriter, r *http.Request) {
	car := chi.URLParam(r, "name")
	skin := r.FormValue("skin-delete")

	if !plainname.Valid(car) {
		http.NotFound(w, r)
		return
	}

	if !plainname.Valid(skin) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// only existing skin directories of this car may be deleted
	if info, err := os.Lstat(filepath.Join(ch.env.InstallPath(), "content", "cars", car, "skins", skin)); err != nil || !info.IsDir() {
		http.NotFound(w, r)
		return
	}

	if err := ch.carManager.DeleteSkin(car, skin); err != nil {
		logrus.WithError(err).Errorf("could not delete car skin for %s", car)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, "Car skin deleted successfully!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *CarsHandler) RebuildSearchIndex(w http.ResponseWriter, r *http.Request) {
	go ch.env.PanicCapture(func() {
		err := ch.carManager.IndexAllCars()

		if err != nil {
			logrus.WithError(err).Error("could not rebuild search index")
		}
	})

	smweb.AddFlash(w, r, "Started re-indexing cars!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type carDetailsTemplateVars struct {
	smweb.BaseTemplateVars

	Car       *smcontent.Car
	Results   []smresults.SessionResults
	Setups    map[string][]string
	TrackOpts []smcontent.Track

	// Records are the best laps of the car per track layout.
	Records []RecordRow
	// FilterServer is the id of the server the records are restricted to, empty for all.
	FilterServer string
}

// loadCarDetailsForTemplate loads all necessary items to generate the car details template.
func (ch *CarsHandler) loadCarDetailsForTemplate(carName string, r *http.Request) (*carDetailsTemplateVars, error) {
	tyres, err := smcontent.ListTyres(ch.env.InstallPath())

	if err != nil {
		return nil, err
	}

	car, err := ch.carManager.LoadCar(carName, tyres)

	if err != nil {
		return nil, err
	}

	results, err := ch.resultsForCar(carName)

	if err != nil {
		return nil, err
	}

	setups, err := smcontent.ListSetupsForCar(ch.env.InstallPath(), carName)

	if err != nil {
		return nil, err
	}

	tracks, err := ch.trackManager.ListTracks()

	if err != nil {
		return nil, err
	}

	recordsFilter, filterServer := ch.records.filter(r)

	return &carDetailsTemplateVars{
		Car:          car,
		Results:      results,
		Setups:       setups,
		TrackOpts:    tracks,
		Records:      ch.records.rows(ch.records.forCar(carName, recordsFilter)),
		FilterServer: filterServer,
	}, nil
}

// resultsForCar finds results for a given car.
func (ch *CarsHandler) resultsForCar(car string) ([]smresults.SessionResults, error) {
	results, err := ch.env.Results().ListAll()

	if err != nil {
		return nil, err
	}

	var out []smresults.SessionResults

	for _, result := range results {
		hasCar := false

		for _, driver := range result.Result {
			if driver.CarModel == car {
				hasCar = true
				break
			}
		}

		if hasCar {
			out = append(out, result)
		}
	}

	return out, nil
}
