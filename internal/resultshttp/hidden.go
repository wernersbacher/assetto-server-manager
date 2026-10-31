package resultshttp

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// HiddenResults knows which results files belong to hidden championships and race weekends. Results are plain files
// which only carry the championship and race weekend id in some cases (not e.g. in the files of a championship's
// sessions before it was linked), so a file is also recognised by the results embedded in the sessions of the
// championship events and race weekends, in the way findResultsReference does.
//
// A nil *HiddenResults hides nothing, it is what NewHiddenResultsFor returns for requesters which may see everything.
type HiddenResults struct {
	files        map[string]bool // SessionFile (file name without .json)
	championship map[string]bool
	raceWeekend  map[string]bool
}

// NewHiddenResultsFor builds the index for a request, once per request. It returns nil if the requester may see
// hidden championships and race weekends (see smweb.CanSeeHidden) or if there is nothing hidden.
func NewHiddenResultsFor(r *http.Request, store smstore.Store) (*HiddenResults, error) {
	if smweb.CanSeeHidden(r) {
		return nil, nil
	}

	return NewHiddenResults(store)
}

// NewHiddenResults reads all championships and race weekends. It returns nil if none is hidden.
func NewHiddenResults(store smstore.Store) (*HiddenResults, error) {
	championships, err := store.ListChampionships()

	if err != nil {
		return nil, err
	}

	raceWeekends, err := store.ListRaceWeekends()

	if err != nil {
		return nil, err
	}

	h := &HiddenResults{
		files:        make(map[string]bool),
		championship: make(map[string]bool),
		raceWeekend:  make(map[string]bool),
	}

	hiddenChampionships := make(map[uuid.UUID]bool)

	for _, championship := range championships {
		if !championship.Hidden {
			continue
		}

		hiddenChampionships[championship.ID] = true
		h.championship[championship.ID.String()] = true

		for _, event := range championship.Events {
			for _, session := range event.Sessions {
				if session.Results != nil {
					h.files[session.Results.SessionFile] = true
				}

				if session.RaceWeekendSession != nil && session.RaceWeekendSession.Results != nil {
					h.files[session.RaceWeekendSession.Results.SessionFile] = true
				}
			}
		}
	}

	for _, raceWeekend := range raceWeekends {
		// a race weekend of a hidden championship is hidden with it
		if !raceWeekend.Hidden && !hiddenChampionships[raceWeekend.ChampionshipID] {
			continue
		}

		h.raceWeekend[raceWeekend.ID.String()] = true

		for _, session := range raceWeekend.Sessions {
			if session.Results != nil {
				h.files[session.Results.SessionFile] = true
			}
		}
	}

	if len(h.files) == 0 && len(h.championship) == 0 && len(h.raceWeekend) == 0 {
		return nil, nil
	}

	return h, nil
}

// Hides tells whether the results file (with or without .json) is to be hidden. championshipID and raceWeekendID are
// the ones stored in the file, they may be empty.
func (h *HiddenResults) Hides(fileName, championshipID, raceWeekendID string) bool {
	if h == nil {
		return false
	}

	return h.files[strings.TrimSuffix(fileName, ".json")] ||
		(championshipID != "" && h.championship[championshipID]) ||
		(raceWeekendID != "" && h.raceWeekend[raceWeekendID])
}

// HidesResult is Hides for loaded results.
func (h *HiddenResults) HidesResult(fileName string, result *smresults.SessionResults) bool {
	return h.Hides(fileName, result.ChampionshipID, result.RaceWeekendID)
}

// HideFunc returns the filter function for the results loader, nil if nothing is hidden.
func (h *HiddenResults) HideFunc() smresults.HideFunc {
	if h == nil {
		return nil
	}

	return h.Hides
}

// hiddenResults builds the index for the request, answering with 500 and returning false on failure.
func (rh *ResultsHandler) hiddenResults(w http.ResponseWriter, r *http.Request) (*HiddenResults, bool) {
	hidden, err := NewHiddenResultsFor(r, rh.store)

	if err != nil {
		logHiddenError(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return nil, false
	}

	return hidden, true
}

func logHiddenError(err error) {
	logrus.WithError(err).Error("couldn't determine hidden championships and race weekends")
}

// denyHiddenResult answers with 404 and returns true if the results belong to a hidden championship or race weekend
// and the requester may not see those. They must look like results which do not exist.
func (rh *ResultsHandler) denyHiddenResult(w http.ResponseWriter, r *http.Request, fileName string, result *smresults.SessionResults) bool {
	hidden, ok := rh.hiddenResults(w, r)

	if !ok {
		return true
	}

	if !hidden.HidesResult(fileName, result) {
		return false
	}

	http.NotFound(w, r)

	return true
}
