package carclasses

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// InstalledCars lists the cars a class can be made of (the cars of the content, as the race builder offers them).
type InstalledCars func(ctx context.Context) (smcontent.Cars, error)

// Handler serves the administration pages of the car classes: list, form to create and edit, delete. Everybody who
// can read sees the classes, the routes that change something are in the write and delete groups of the router. The
// rules are the Manager's, the handler only shows its refusals instead of failing with a server error.
type Handler struct {
	*smweb.BaseHandler

	manager *Manager
	cars    InstalledCars
}

func NewHandler(baseHandler *smweb.BaseHandler, manager *Manager, cars InstalledCars) *Handler {
	return &Handler{BaseHandler: baseHandler, manager: manager, cars: cars}
}

// ListPath is the page with all car classes. The page of one class is ClassPath, it is also the target of the
// forms which change or delete it; NewPath is the form for a new class.
const (
	ListPath = "/car-classes"
	NewPath  = "/car-classes/new"
)

// ClassPath returns the path of the page of the car class.
func ClassPath(class *smmodel.CarClass) string {
	return ListPath + "/" + class.ID.String()
}

type listItem struct {
	Class *smmodel.CarClass
	// Championships is the number of championships that use the class.
	Championships int
}

type classListVars struct {
	smweb.BaseTemplateVars

	Classes []listItem
}

type classFormVars struct {
	smweb.BaseTemplateVars

	// Class is nil on the page of a new class.
	Class *smmodel.CarClass

	// Name and ValidCarIDs are what the form shows: the class, or the rejected input.
	Name        string
	ValidCarIDs []string
	CarOpts     smcontent.Cars

	// Usages are the championship classes that use the class.
	Usages []Usage

	// FormError is the translated reason why the last change was refused.
	FormError string
}

// Championships lists the usages with each championship once (the first of its classes).
func (v *classFormVars) Championships() []Usage {
	return firstPerChampionship(v.Usages)
}

func firstPerChampionship(usages []Usage) []Usage {
	var out []Usage

	seen := make(map[string]bool)

	for _, usage := range usages {
		if !seen[usage.ChampionshipID.String()] {
			seen[usage.ChampionshipID.String()] = true
			out = append(out, usage)
		}
	}

	return out
}

func (h *Handler) serverError(w http.ResponseWriter, err error, what string) {
	logrus.WithError(err).Error(what)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// find returns the car class of the request, or answers with 404 (500 for other errors).
func (h *Handler) find(w http.ResponseWriter, r *http.Request) (*smmodel.CarClass, bool) {
	class, err := h.manager.Get(chi.URLParam(r, "classID"))

	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)

		return nil, false
	} else if err != nil {
		h.serverError(w, err, "couldn't load car class")

		return nil, false
	}

	return class, true
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	classes, err := h.manager.List()

	if err != nil {
		h.serverError(w, err, "couldn't list car classes")

		return
	}

	vars := &classListVars{}

	for _, class := range classes {
		usages, err := h.manager.Usages(class.ID.String())

		if err != nil {
			h.serverError(w, err, "couldn't list the usages of a car class")

			return
		}

		vars.Classes = append(vars.Classes, listItem{Class: class, Championships: len(firstPerChampionship(usages))})
	}

	h.ViewRenderer.MustLoadTemplate(w, r, "car-classes/list.html", vars)
}

// New shows the empty form.
func (h *Handler) New(w http.ResponseWriter, r *http.Request) {
	h.renderForm(w, r, &classFormVars{})
}

// View shows the form of a class with the championships that use it.
func (h *Handler) View(w http.ResponseWriter, r *http.Request) {
	class, ok := h.find(w, r)

	if !ok {
		return
	}

	h.renderForm(w, r, &classFormVars{Class: class, Name: class.Name, ValidCarIDs: class.Cars})
}

// renderForm adds the usages of the class and the installed cars to the vars and renders the form.
func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, vars *classFormVars) {
	if vars.Class != nil {
		usages, err := h.manager.Usages(vars.Class.ID.String())

		if err != nil {
			h.serverError(w, err, "couldn't load the usages of a car class")

			return
		}

		vars.Usages = usages
	}

	cars, err := h.cars(r.Context())

	if err != nil {
		h.serverError(w, err, "couldn't list cars")

		return
	}

	vars.CarOpts = withSelected(cars, vars.ValidCarIDs)

	h.ViewRenderer.MustLoadTemplate(w, r, "car-classes/form.html", vars)
}

// withSelected makes sure that every selected car is an option, also a car that is no longer installed (otherwise
// saving the class would silently drop it).
func withSelected(cars smcontent.Cars, selected []string) smcontent.Cars {
	have := make(map[string]bool, len(cars))

	for _, car := range cars {
		have[car.Name] = true
	}

	out := append(smcontent.Cars(nil), cars...)

	for _, name := range selected {
		if !have[name] {
			have[name] = true
			out = append(out, &smcontent.Car{Name: name})
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// formInput reads the name and the selected cars of the form.
func formInput(r *http.Request) (name string, cars []string) {
	_ = r.ParseForm()

	return r.PostForm.Get("Name"), r.PostForm["Cars"]
}

// refused shows the form again with the input and the reason (an Error of the manager, ErrorText translates it).
// Other errors are server errors.
func (h *Handler) refused(w http.ResponseWriter, r *http.Request, class *smmodel.CarClass, name string, cars []string, err error) {
	var refusal Error

	if !errors.As(err, &refusal) {
		h.serverError(w, err, "couldn't save car class")

		return
	}

	w.WriteHeader(http.StatusUnprocessableEntity)
	h.renderForm(w, r, &classFormVars{Class: class, Name: name, ValidCarIDs: cars, FormError: smweb.ErrorText(r, err)})
}

// Create saves a new class.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	name, cars := formInput(r)

	class, err := h.manager.Create(name, cars)

	if err != nil {
		h.refused(w, r, nil, name, cars, err)

		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.carclass.created", "name", class.Name))
	http.Redirect(w, r, ClassPath(class), http.StatusFound)
}

// Update changes name and cars of the class.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	class, ok := h.find(w, r)

	if !ok {
		return
	}

	name, cars := formInput(r)

	updated, err := h.manager.Update(class.ID.String(), name, cars)

	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)

		return
	} else if err != nil {
		h.refused(w, r, class, name, cars, err)

		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.carclass.updated", "name", updated.Name))
	http.Redirect(w, r, ClassPath(updated), http.StatusFound)
}

// Delete removes a class that no championship uses. A refusal is flashed and brings the user back to the class.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	class, ok := h.find(w, r)

	if !ok {
		return
	}

	err := h.manager.Delete(class.ID.String())

	var refusal Error

	switch {
	case errors.Is(err, ErrNotFound):
		http.NotFound(w, r)
	case errors.As(err, &refusal):
		smweb.AddErrorFlash(w, r, smweb.ErrorText(r, err))
		http.Redirect(w, r, ClassPath(class), http.StatusFound)
	case err != nil:
		h.serverError(w, err, "couldn't delete car class")
	default:
		smweb.AddFlash(w, r, smweb.T(r, "flash.carclass.deleted", "name", class.Name))
		http.Redirect(w, r, ListPath, http.StatusFound)
	}
}

// ListAPI lists all car classes in the API format, sorted by name.
func (h *Handler) ListAPI(w http.ResponseWriter, r *http.Request) {
	classes, err := h.manager.List()

	if err != nil {
		h.serverError(w, err, "couldn't list car classes")

		return
	}

	out := make([]*api.CarClass, 0, len(classes))

	for _, class := range classes {
		out = append(out, api.NewCarClass(class))
	}

	api.WriteJSON(w, out)
}

// ViewAPI returns one car class in the API format (404 for an unknown class or an ID that is no UUID).
func (h *Handler) ViewAPI(w http.ResponseWriter, r *http.Request) {
	class, ok := h.find(w, r)

	if !ok {
		return
	}

	api.WriteJSON(w, api.NewCarClass(class))
}
