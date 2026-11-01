package race

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// Export returns a custom race in JSON format (see ExportedCustomRace).
func (crh *CustomRaceHandler) Export(w http.ResponseWriter, r *http.Request) {
	manager, ok := crh.manager(w, r)

	if !ok {
		return
	}

	// the ID ends up in a file name of the stores
	id, err := uuid.Parse(chi.URLParam(r, "uuid"))

	if err != nil {
		http.NotFound(w, r)
		return
	}

	// the route needs write access, so the replacement password may be exported
	race, err := manager.ExportCustomRace(id.String(), true)

	if errors.Is(err, smstore.ErrCustomRaceNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("couldn't export custom race")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	name := race.Name

	if name == "" {
		name = "custom-race"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Add("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name+".json"))

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(race)
}

// Import shows the import form and saves the custom race of a pasted export.
func (crh *CustomRaceHandler) Import(w http.ResponseWriter, r *http.Request) {
	manager, ok := crh.manager(w, r)

	if !ok {
		return
	}

	if r.Method == http.MethodPost {
		_, err := manager.ImportCustomRace(r.FormValue("import"))

		var validationErrors ValidationErrors
		var importErr ImportError

		switch {
		case err == nil:
			smweb.AddFlash(w, r, smweb.T(r, "flash.race.custom_imported"))
			http.Redirect(w, r, smweb.ServerURL(r, "/custom"), http.StatusFound)
			return
		case errors.As(err, &importErr):
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.race.custom_import_invalid", "error", smweb.TM(r, importErr.Message)))
		case errors.As(err, &validationErrors):
			texts := ""

			for i, message := range validationErrors.Messages() {
				if i > 0 {
					texts += " "
				}

				texts += smweb.TM(r, message)
			}

			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.race.custom_import_invalid", "error", texts))
		default:
			logrus.WithError(err).Errorf("couldn't import custom race")
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.race.custom_import_failed"))
		}
	}

	crh.ViewRenderer.MustLoadTemplate(w, r, "custom-race/import.html", nil)
}
