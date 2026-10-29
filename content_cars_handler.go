package servermanager

import (
	"github.com/wernersbacher/assetto-server-manager/internal/plainname"

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
)

type CarsHandler struct {
	*BaseHandler

	carManager *CarManager
}

func NewCarsHandler(baseHandler *BaseHandler, carManager *CarManager) *CarsHandler {
	return &CarsHandler{
		BaseHandler: baseHandler,
		carManager:  carManager,
	}
}

type carListTemplateVars struct {
	BaseTemplateVars

	Results     *bleve.SearchResult
	Cars        Cars
	Query       string
	CurrentPage int
	NumPages    int
	PageSize    int
}

func (ch *CarsHandler) list(w http.ResponseWriter, r *http.Request) {
	searchTerm := r.URL.Query().Get("q")
	page := formValueAsInt(r.URL.Query().Get("page"))
	results, cars, err := ch.carManager.Search(r.Context(), searchTerm, page*searchPageSize, searchPageSize)

	if err != nil {
		logrus.WithError(err).Error("Could not perform search")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	numPages := int(math.Ceil(float64(results.Total) / float64(searchPageSize)))

	ch.viewRenderer.MustLoadTemplate(w, r, "content/cars.html", &carListTemplateVars{
		Results:     results,
		Cars:        cars,
		Query:       searchTerm,
		CurrentPage: page,
		NumPages:    numPages,
		PageSize:    searchPageSize,
	})
}

type carSearchResult struct {
	CarName string `json:"CarName"`
	CarID   string `json:"CarID"`
	Class   string `json:"Class"`
	// Tags    []string `json:"Tags"`
}

func (ch *CarsHandler) searchJSON(w http.ResponseWriter, r *http.Request) {
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
	if Debug {
		enc.SetIndent("", "    ")
	}
	_ = enc.Encode(searchResults)
}

func (ch *CarsHandler) delete(w http.ResponseWriter, r *http.Request) {
	carName := chi.URLParam(r, "name")
	err := ch.carManager.DeleteCar(carName)

	if err != nil {
		logrus.WithError(err).Errorf("Could not delete car: %s", carName)
		AddErrorFlash(w, r, "couldn't get car list")
		http.Redirect(w, r, r.Referer(), http.StatusFound)
		return
	}

	AddFlash(w, r, fmt.Sprintf("Car %s successfully deleted!", carName))
	http.Redirect(w, r, "/cars", http.StatusFound)
}

const defaultSkinURL = "/static/img/no-preview-car.png"

func carSkinURL(car, skin string) string {
	skinPath := filepath.Join("content", "cars", car, "skins", url.PathEscape(skin), "preview.jpg")

	// look to see if the car preview image exists
	_, err := os.Stat(filepath.Join(ServerInstallPath, filepath.Join("content", "cars", car, "skins", skin, "preview.jpg")))

	if err != nil {
		return defaultSkinURL
	}

	return "/" + filepath.ToSlash(skinPath)
}

func (ch *CarsHandler) view(w http.ResponseWriter, r *http.Request) {
	carName := chi.URLParam(r, "car_id")

	if !plainname.Valid(carName) {
		http.NotFound(w, r)
		return
	}

	templateParams, err := ch.carManager.loadCarDetailsForTemplate(carName)

	if os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("Could not load car details for: %s", carName)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	ch.viewRenderer.MustLoadTemplate(w, r, "content/car-details.html", templateParams)
}

func (ch *CarsHandler) tags(w http.ResponseWriter, r *http.Request) {
	car := chi.URLParam(r, "name")

	if !plainname.Valid(car) {
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodPost {
		tag := r.FormValue("new-tag")
		err := ch.carManager.AddTag(car, tag)

		if err == nil {
			AddFlash(w, r, fmt.Sprintf("Successfully added the tag: %s", tag))
		} else {
			AddFlash(w, r, "Could not add tag")
		}
	} else {
		tag := r.URL.Query().Get("delete")
		err := ch.carManager.DelTag(car, tag)

		if err == nil {
			AddFlash(w, r, fmt.Sprintf("Successfully deleted the tag: %s", tag))
		} else {
			AddFlash(w, r, "Could not delete tag")
		}
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *CarsHandler) saveMetadata(w http.ResponseWriter, r *http.Request) {
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

	AddFlash(w, r, "Car metadata updated successfully!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *CarsHandler) uploadSkin(w http.ResponseWriter, r *http.Request) {
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

	AddFlash(w, r, "Car skin uploaded successfully!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *CarsHandler) deleteSkin(w http.ResponseWriter, r *http.Request) {
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
	if info, err := os.Lstat(filepath.Join(ServerInstallPath, "content", "cars", car, "skins", skin)); err != nil || !info.IsDir() {
		http.NotFound(w, r)
		return
	}

	if err := ch.carManager.DeleteSkin(car, skin); err != nil {
		logrus.WithError(err).Errorf("could not delete car skin for %s", car)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	AddFlash(w, r, "Car skin deleted successfully!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (ch *CarsHandler) rebuildSearchIndex(w http.ResponseWriter, r *http.Request) {
	go panicCapture(func() {
		err := ch.carManager.IndexAllCars()

		if err != nil {
			logrus.WithError(err).Error("could not rebuild search index")
		}
	})

	AddFlash(w, r, "Started re-indexing cars!")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}
