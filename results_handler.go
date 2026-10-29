package servermanager

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
)

type ResultsHandler struct {
	*BaseHandler

	store Store
}

func NewResultsHandler(baseHandler *BaseHandler, store Store) *ResultsHandler {
	return &ResultsHandler{
		BaseHandler: baseHandler,
		store:       store,
	}
}

type resultsListTemplateVars struct {
	BaseTemplateVars

	Results     []SessionResults
	Pages       []int
	CurrentPage int
	Filter      ResultsFilter
}

// resultsListQuery reads the page and filters of a results list request, which is shared by the results list page
// and the results API.
func resultsListQuery(r *http.Request) (int, ResultsFilter) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))

	if err != nil {
		page = 0
	}

	return page, ResultsFilter{
		Type:   strings.TrimSpace(r.URL.Query().Get("type")),
		Track:  strings.TrimSpace(r.URL.Query().Get("track")),
		Driver: strings.TrimSpace(r.URL.Query().Get("driver")),
	}
}

func (rh *ResultsHandler) list(w http.ResponseWriter, r *http.Request) {
	page, filter := resultsListQuery(r)

	results, pages, err := listResults(page, filter)

	if err == ErrResultsPageNotFound {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rh.viewRenderer.MustLoadTemplate(w, r, "results/index.html", &resultsListTemplateVars{
		Results:     results,
		Pages:       pages,
		CurrentPage: page,
		Filter:      filter,
	})
}

func (rh *ResultsHandler) uploadHandler(w http.ResponseWriter, r *http.Request) {
	matched, err := rh.upload(r)

	if err != nil {
		logrus.WithError(err).Errorf("could not parse results form")
		AddErrorFlash(w, r, "Sorry, we couldn't parse that results file! Please make sure the format is correct.")
	}

	if !matched {
		AddErrorFlash(w, r, "Your results file content was correct, but the file name is incorrect! Please make sure the file name matches the AC standard then try again.")
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

type combineResultsTemplateVars struct {
	BaseTemplateVars

	Results []SessionResults
}

const combinedSuffix = "-combined"

func (rh *ResultsHandler) combineResults(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			logrus.WithError(err).Error("Combine Results: Couldn't parse form")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		combineResultsSessionFiles := r.Form["CombineResults"]

		var resultsToCombine []*SessionResults

		for _, resultSessionFile := range combineResultsSessionFiles {
			result, err := LoadResult(resultSessionFile + ".json")

			if err != nil {
				logrus.WithError(err).Error("Combine Results: Couldn't load result")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			resultsToCombine = append(resultsToCombine, result)
		}

		combinedResult := combineResults(resultsToCombine)

		combinedResult.FallBackSort()
		combinedResult.UpdateDate(time.Now())

		if !strings.HasSuffix(combinedResult.SessionFile, combinedSuffix) {
			combinedResult.SessionFile = combinedResult.SessionFile + combinedSuffix
		}

		err := saveResults(combinedResult.SessionFile+".json", combinedResult)

		if err != nil {
			logrus.WithError(err).Error("Combine Results: Couldn't save result")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/results", http.StatusFound)
		return
	}

	results, err := ListAllResults()

	if err != nil {
		logrus.WithError(err).Error("Combine Results: Couldn't list results")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rh.viewRenderer.MustLoadTemplate(w, r, "results/combine.html", &combineResultsTemplateVars{
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
	var resultTest *SessionResults

	err = json.Unmarshal(fileBytes, &resultTest)

	if err != nil {
		return true, err
	}

	matched, err := regexp.MatchString(`\d{4}_\d{1,2}_\d{1,2}_\d{1,2}_\d{1,2}_(RACE|QUALIFY|PRACTICE|BOOK)\.json`, header.Filename)

	if err != nil {
		return matched, err
	}

	if !matched || !validResultsFileName(header.Filename) {
		return false, nil
	}

	path := filepath.Join(ServerInstallPath, "results", header.Filename)

	err = ioutil.WriteFile(path, fileBytes, 0644)

	if err != nil {
		return matched, err
	}

	return matched, nil
}

type resultsViewTemplateVars struct {
	BaseTemplateVars

	Result           *SessionResults
	AutoFillEntrants []*Entrant
	Account          *Account
	UseMPH           bool
}

func (rh *ResultsHandler) view(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !validResultsFileName(fileName + ".json") {
		http.NotFound(w, r)
		return
	}

	result, err := LoadResult(fileName + ".json")

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

	rh.viewRenderer.MustLoadTemplate(w, r, "results/result.html", &resultsViewTemplateVars{
		BaseTemplateVars: BaseTemplateVars{
			WideContainer: true,
		},
		Result:           result,
		AutoFillEntrants: autoFillEntrants,
		Account:          AccountFromRequest(r),
		UseMPH:           serverOpts.UseMPH == 1,
	})
}

func (rh *ResultsHandler) file(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !validResultsFileName(fileName) {
		http.NotFound(w, r)
		return
	}

	result, err := LoadResult(fileName)

	if os.IsNotExist(err) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if UseShortenedDriverNames {
		result.MaskDriverNames()
	}

	w.Header().Add("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

func (rh *ResultsHandler) edit(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !validResultsFileName(fileName + ".json") {
		http.NotFound(w, r)
		return
	}

	results, err := LoadResult(fileName + ".json")

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

	err = saveResults(results.SessionFile+".json", results)

	if err != nil {
		logrus.WithError(err).Error("could not load results")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	AddFlash(w, r, "Drivers successfully edited")
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (rh *ResultsHandler) delete(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	err := deleteResultsFile(rh.store, fileName)

	var referencedErr ResultsFileReferencedError

	switch {
	case err == ErrInvalidResultsFileName || os.IsNotExist(err):
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	case errors.As(err, &referencedErr):
		logrus.WithError(err).Warnf("refusing to delete results file: %s", fileName)
		AddErrorFlash(w, r, fmt.Sprintf("Results file could not be deleted, it is used by %s", referencedErr.Reference))
	case err != nil:
		logrus.WithError(err).Errorf("could not delete results file: %s", fileName)
		AddErrorFlash(w, r, "Unable to delete the results file")
	default:
		AddFlash(w, r, "Results file successfully deleted")
	}

	http.Redirect(w, r, "/results", http.StatusFound)
}
