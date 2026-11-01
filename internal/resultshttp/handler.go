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

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/plainname"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// ResultsHandler serves the results list, the results of a session, the results API and the collision maps, and
// uploads, combines, edits and deletes results.
type ResultsHandler struct {
	*smweb.BaseHandler

	store   smstore.Store
	env     *serverenv.Env
	servers ServersFunc

	// CarClasses resolves the carClass parameter of the list (nil: there are no car classes, every carClass is
	// unknown). The resolver sets it.
	CarClasses smweb.CarClasses
}

// NewResultsHandler creates the handler. The results are read from all results directories of the env; servers, which
// may be nil, attributes them to the server slots.
func NewResultsHandler(baseHandler *smweb.BaseHandler, store smstore.Store, env *serverenv.Env, servers ServersFunc) *ResultsHandler {
	return &ResultsHandler{
		BaseHandler: baseHandler,
		store:       store,
		env:         env,
		servers:     servers,
	}
}

type resultsListTemplateVars struct {
	smweb.BaseTemplateVars

	Results     []smresults.SessionResults
	Pages       []int
	CurrentPage int
	Filter      smresults.Filter

	// FilterServer is the id of the server the list is restricted to, empty for all. ServerNames maps the SessionFile of
	// a result to the name of the server it came from; both are only set if there are several servers.
	FilterServer string
	ServerNames  map[string]string

	// CarClasses are the classes to choose from (empty: there are none).
	CarClasses []smweb.CarClassOption
}

// resultsListQuery is the page and filters of a results list request.
type resultsListQuery struct {
	page         int
	filter       smresults.Filter
	servers      serverIndex
	filterServer string

	// carClasses are the canonical IDs of the classes asked for.
	carClasses []string
}

// parseListQuery reads the page and filters of a results list request, which is shared by the results list page
// and the results API. carClass and car restrict the list to sessions with a car of any chosen class or any chosen car
// (a union); an unknown class is an *smweb.UnknownCarClassError, never an empty or an unfiltered list.
func (rh *ResultsHandler) parseListQuery(r *http.Request) (*resultsListQuery, error) {
	values := r.URL.Query()

	page, err := strconv.Atoi(values.Get("page"))

	if err != nil {
		page = 0
	}

	selection, err := smweb.ResolveCarClasses(rh.CarClasses, values["carClass"])

	if err != nil {
		return nil, err
	}

	var cars []string

	for _, car := range values["car"] {
		if car == "" {
			continue
		}

		if !plainname.Valid(car) {
			return nil, fmt.Errorf("car must be a valid name")
		}

		cars = append(cars, car)
	}

	q := &resultsListQuery{
		page: page,
		filter: smresults.Filter{
			Type:   strings.TrimSpace(values.Get("type")),
			Track:  strings.TrimSpace(values.Get("track")),
			Driver: strings.TrimSpace(values.Get("driver")),
			Cars:   selection.UnionCars(cars),
		},
		carClasses: selection.IDs,
		servers:    rh.serverIndex(),
	}

	q.filterServer = q.servers.filterServer(r, &q.filter)

	return q, nil
}

func (rh *ResultsHandler) List(w http.ResponseWriter, r *http.Request) {
	q, err := rh.parseListQuery(r)

	if smweb.FilterError(w, err) {
		return
	}

	page, filter, servers, filterServer := q.page, q.filter, q.servers, q.filterServer

	hidden, ok := rh.hiddenResults(w, r)

	if !ok {
		return
	}

	filter.Hide = hidden.HideFunc()

	results, pages, err := rh.env.Results().List(page, filter)

	if err == smresults.ErrPageNotFound {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var serverNames map[string]string

	if len(servers.servers) > 0 {
		serverNames = make(map[string]string)

		for _, result := range results {
			if server := servers.of(rh.env.Results(), result.SessionFile+".json"); server != nil {
				serverNames[result.SessionFile] = server.Name
			}
		}
	}

	rh.ViewRenderer.MustLoadTemplate(w, r, "results/index.html", &resultsListTemplateVars{
		Results:      results,
		Pages:        pages,
		CurrentPage:  page,
		Filter:       filter,
		FilterServer: filterServer,
		ServerNames:  serverNames,
		CarClasses:   smweb.CarClassOptions(rh.CarClasses, q.carClasses),
	})
}

func (rh *ResultsHandler) UploadHandler(w http.ResponseWriter, r *http.Request) {
	matched, err := rh.upload(r)

	if err != nil {
		logrus.WithError(err).Errorf("could not parse results form")
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.results.parse_failed"))
	}

	if !matched {
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.results.bad_file_name"))
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

	// acServer names results after the session start, so a file of this name from another server would be hidden by
	// (or hide) the uploaded one
	if rh.env.Results().HeldByOtherDir(header.Filename) {
		return true, fmt.Errorf("servermanager: a results file named %s already exists in the results of another server", header.Filename)
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

	// Server is the server the result came from, only set if there are several servers.
	Server *smweb.ServerRef
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

	if rh.denyHiddenResult(w, r, fileName, result) {
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
		Server:           rh.serverIndex().of(rh.env.Results(), fileName+".json"),
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

	if rh.denyHiddenResult(w, r, fileName, result) {
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

	smweb.AddFlash(w, r, smweb.T(r, "flash.results.drivers_edited"))
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
		if referencedErr.Championship {
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.results.used_by_championship", "name", referencedErr.Name))
		} else {
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.results.used_by_race_weekend", "name", referencedErr.Name))
		}
	case err != nil:
		logrus.WithError(err).Errorf("could not delete results file: %s", fileName)
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.results.not_deleted"))
	default:
		smweb.AddFlash(w, r, smweb.T(r, "flash.results.deleted"))
	}

	http.Redirect(w, r, "/results", http.StatusFound)
}
