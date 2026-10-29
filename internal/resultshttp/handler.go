package resultshttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// ResultsHandler serves the results list, the results of a session, the results API and the collision maps, and
// uploads, combines, edits and deletes results.
type ResultsHandler struct {
	*smweb.BaseHandler

	store smstore.Store
	env   *serverenv.Env
}

func NewResultsHandler(baseHandler *smweb.BaseHandler, store smstore.Store, env *serverenv.Env) *ResultsHandler {
	return &ResultsHandler{
		BaseHandler: baseHandler,
		store:       store,
		env:         env,
	}
}

type resultsListTemplateVars struct {
	smweb.BaseTemplateVars

	Results     []smresults.SessionResults
	Pages       []int
	CurrentPage int
	Filter      smresults.Filter
}

// resultsListQuery reads the page and filters of a results list request, which is shared by the results list page
// and the results API.
func resultsListQuery(r *http.Request) (int, smresults.Filter) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))

	if err != nil {
		page = 0
	}

	return page, smresults.Filter{
		Type:   strings.TrimSpace(r.URL.Query().Get("type")),
		Track:  strings.TrimSpace(r.URL.Query().Get("track")),
		Driver: strings.TrimSpace(r.URL.Query().Get("driver")),
	}
}

func (rh *ResultsHandler) List(w http.ResponseWriter, r *http.Request) {
	page, filter := resultsListQuery(r)

	results, pages, err := rh.env.Results().List(page, filter)

	if err == smresults.ErrPageNotFound {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rh.ViewRenderer.MustLoadTemplate(w, r, "results/index.html", &resultsListTemplateVars{
		Results:     results,
		Pages:       pages,
		CurrentPage: page,
		Filter:      filter,
	})
}

func (rh *ResultsHandler) UploadHandler(w http.ResponseWriter, r *http.Request) {
	matched, err := rh.upload(r)

	if err != nil {
		logrus.WithError(err).Errorf("could not parse results form")
		smweb.AddErrorFlash(w, r, "Sorry, we couldn't parse that results file! Please make sure the format is correct.")
	}

	if !matched {
		smweb.AddErrorFlash(w, r, "Your results file content was correct, but the file name is incorrect! Please make sure the file name matches the AC standard then try again.")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type combineResultsTemplateVars struct {
	smweb.BaseTemplateVars

	Results []smresults.SessionResults
}

const combinedSuffix = "-combined"

func (rh *ResultsHandler) CombineResults(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			logrus.WithError(err).Error("Combine Results: Couldn't parse form")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		combineResultsSessionFiles := r.Form["CombineResults"]

		var resultsToCombine []*smresults.SessionResults

		for _, resultSessionFile := range combineResultsSessionFiles {
			result, err := rh.env.Results().Load(resultSessionFile + ".json")

			if err != nil {
				logrus.WithError(err).Error("Combine Results: Couldn't load result")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			resultsToCombine = append(resultsToCombine, result)
		}

		combinedResult := smresults.Combine(resultsToCombine)

		combinedResult.FallBackSort()
		combinedResult.UpdateDate(time.Now())

		if !strings.HasSuffix(combinedResult.SessionFile, combinedSuffix) {
			combinedResult.SessionFile = combinedResult.SessionFile + combinedSuffix
		}

		err := rh.env.Results().Save(combinedResult.SessionFile+".json", combinedResult)

		if err != nil {
			logrus.WithError(err).Error("Combine Results: Couldn't save result")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/results", http.StatusFound)
		return
	}

	results, err := rh.env.Results().ListAll()

	if err != nil {
		logrus.WithError(err).Error("Combine Results: Couldn't list results")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rh.ViewRenderer.MustLoadTemplate(w, r, "results/combine.html", &combineResultsTemplateVars{
		Results: results,
	})
}

const uploadFileSizeLimit = 5e6

func (rh *ResultsHandler) upload(r *http.Request) (bool, error) {
	err := r.ParseMultipartForm(10 << 20)

	if err != nil {
		return true, err
	}

	file, header, err := r.FormFile("resultsFile")
	if err != nil {
		return true, err
	}
	defer file.Close()

	if header.Size > (uploadFileSizeLimit) {
		return true, fmt.Errorf("servermanager: file size too large, limit is: %d, this file is: %d", int64(uploadFileSizeLimit), header.Size)
	}

	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		return true, err
	}

	// make sure what we've been given is actually a results file
	var resultTest *smresults.SessionResults

	err = json.Unmarshal(fileBytes, &resultTest)

	if err != nil {
		return true, err
	}

	matched, err := regexp.MatchString(`\d{4}_\d{1,2}_\d{1,2}_\d{1,2}_\d{1,2}_(RACE|QUALIFY|PRACTICE|BOOK)\.json`, header.Filename)

	if err != nil {
		return matched, err
	}

	if !matched || !smresults.ValidFileName(header.Filename) {
		return false, nil
	}

	path := filepath.Join(rh.env.InstallPath(), "results", header.Filename)

	err = ioutil.WriteFile(path, fileBytes, 0644)

	if err != nil {
		return matched, err
	}

	return matched, nil
}

type resultsViewTemplateVars struct {
	smweb.BaseTemplateVars

	Result           *smresults.SessionResults
	AutoFillEntrants []*serverconfig.Entrant
	Account          *smmodel.Account
	UseMPH           bool
}

func (rh *ResultsHandler) View(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !smresults.ValidFileName(fileName + ".json") {
		http.NotFound(w, r)
		return
	}

	result, err := rh.env.Results().Load(fileName + ".json")

	if os.IsNotExist(err) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	serverOpts, err := rh.store.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load server options")
	}

	autoFillEntrants, err := rh.store.ListEntrants()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load autofill entrant list")
	}

	result.ClearKickedGUIDs()
	result.NormaliseCarIDs()

	rh.ViewRenderer.MustLoadTemplate(w, r, "results/result.html", &resultsViewTemplateVars{
		BaseTemplateVars: smweb.BaseTemplateVars{
			WideContainer: true,
		},
		Result:           result,
		AutoFillEntrants: autoFillEntrants,
		Account:          smweb.AccountFromRequest(r),
		UseMPH:           serverOpts.UseMPH == 1,
	})
}

func (rh *ResultsHandler) File(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !smresults.ValidFileName(fileName) {
		http.NotFound(w, r)
		return
	}

	result, err := rh.env.Results().Load(fileName)

	if os.IsNotExist(err) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if smresults.UseShortenedDriverNames {
		result.MaskDriverNames()
	}

	w.Header().Add("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

func (rh *ResultsHandler) Edit(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !smresults.ValidFileName(fileName + ".json") {
		http.NotFound(w, r)
		return
	}

	results, err := rh.env.Results().Load(fileName + ".json")

	if os.IsNotExist(err) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Error("could not load results")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	for key, vals := range r.Form {
		if strings.HasPrefix(key, "guid:") {
			guid := strings.TrimPrefix(key, "guid:")
			name := vals[0]

			results.RenameDriver(guid, name)
		}
	}

	err = rh.env.Results().Save(results.SessionFile+".json", results)

	if err != nil {
		logrus.WithError(err).Error("could not load results")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, "Drivers successfully edited")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (rh *ResultsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	err := deleteResultsFile(rh.env, rh.store, fileName)

	var referencedErr ResultsFileReferencedError

	switch {
	case err == smresults.ErrInvalidFileName || os.IsNotExist(err):
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	case errors.As(err, &referencedErr):
		logrus.WithError(err).Warnf("refusing to delete results file: %s", fileName)
		smweb.AddErrorFlash(w, r, fmt.Sprintf("Results file could not be deleted, it is used by %s", referencedErr.Reference))
	case err != nil:
		logrus.WithError(err).Errorf("could not delete results file: %s", fileName)
		smweb.AddErrorFlash(w, r, "Unable to delete the results file")
	default:
		smweb.AddFlash(w, r, "Results file successfully deleted")
	}

	http.Redirect(w, r, "/results", http.StatusFound)
}
