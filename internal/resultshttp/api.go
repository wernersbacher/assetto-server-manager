package resultshttp

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	"github.com/wernersbacher/assetto-server-manager/internal/api"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

// The results API exposes the information that is publicly visible on the results pages (session, track, drivers,
// classification and laps) in a stable JSON format, so that other systems can read it. All times and durations are in
// milliseconds, as Assetto Corsa stores them. Driver names are shortened if the server is set to hide last names.
// Collisions, the raw results file and any server configuration are intentionally left out.

type APIResultsList struct {
	// Page is the zero-based number of the returned page, Pages the total number of pages.
	Page  int
	Pages int

	Results []*APIResultSummary
}

type APIResultSummary struct {
	// FileName is the name of the results file, as used by /results/download/{fileName}. /results/{fileName} and
	// /api/results/{fileName} work with and without the .json extension.
	FileName string

	Date        time.Time
	SessionType serverconfig.SessionType
	TimeAttack  bool

	Track       string
	TrackLayout string
	TrackName   string

	Drivers []string

	// ChampionshipID and RaceWeekendID are only set for sessions that were driven as part of one.
	ChampionshipID string `json:",omitempty"`
	RaceWeekendID  string `json:",omitempty"`
}

type APIResult struct {
	APIResultSummary

	Classification []*APIResultEntry
	Laps           []*APILap
}

type APIResultEntry struct {
	Position int

	DriverName string
	DriverGUID string
	Team       string `json:",omitempty"`

	CarModel string
	CarName  string

	// BestLap and TotalTime are in milliseconds. TotalTime is 0 for sessions without a race time.
	BestLap   int
	TotalTime int
	// Laps is the number of laps the driver is credited with, after lap penalties.
	Laps int

	HasPenalty bool
	// PenaltyTime is in milliseconds.
	PenaltyTime  int64 `json:",omitempty"`
	LapPenalty   int   `json:",omitempty"`
	Disqualified bool
}

type APILap struct {
	// Number is the lap number of the car, starting at 1.
	Number int

	DriverName string
	DriverGUID string
	CarModel   string

	// LapTime and Sectors are in milliseconds.
	LapTime int
	Sectors []int
	Cuts    int
	Tyre    string
}

func newAPIResultSummary(installPath string, result *smresults.SessionResults) *APIResultSummary {
	out := &APIResultSummary{
		FileName:       result.SessionFile + ".json",
		Date:           result.Date,
		SessionType:    result.Type,
		TimeAttack:     result.IsTimeAttack(),
		Track:          result.TrackName,
		TrackLayout:    result.TrackConfig,
		TrackName:      api.TrackName(installPath, result.TrackName, result.TrackConfig),
		Drivers:        []string{},
		ChampionshipID: result.ChampionshipID,
		RaceWeekendID:  result.RaceWeekendID,
	}

	for _, driver := range result.Result {
		if driver.DriverName != "" {
			out.Drivers = append(out.Drivers, smresults.DriverName(driver.DriverName))
		}
	}

	return out
}

// NewAPIResult converts a results file to the API format. The results should be prepared in the same way as for the
// results page (see ResultsHandler.view).
func NewAPIResult(installPath string, result *smresults.SessionResults) *APIResult {
	out := &APIResult{
		APIResultSummary: *newAPIResultSummary(installPath, result),
		Classification:   []*APIResultEntry{},
		Laps:             []*APILap{},
	}

	for i, entry := range result.Result {
		apiEntry := &APIResultEntry{
			Position:     i + 1,
			DriverName:   smresults.DriverName(entry.DriverName),
			DriverGUID:   entry.DriverGUID,
			Team:         result.GetTeamName(entry.DriverGUID),
			CarModel:     entry.CarModel,
			CarName:      smcontent.PrettifyName(entry.CarModel, true),
			BestLap:      entry.BestLap,
			TotalTime:    entry.TotalTime,
			Laps:         result.GetNumLaps(entry.DriverGUID, entry.CarModel),
			HasPenalty:   entry.HasPenalty,
			PenaltyTime:  entry.PenaltyTime.Nanoseconds() / int64(time.Millisecond),
			LapPenalty:   entry.LapPenalty,
			Disqualified: entry.Disqualified,
		}

		// drivers in a driver swap share a car, the team is stored on the car
		if car, err := result.FindCarByGUIDAndModel(entry.DriverGUID, entry.CarModel); err == nil && apiEntry.Team == "" {
			apiEntry.Team = car.GetTeam()
		}

		out.Classification = append(out.Classification, apiEntry)
	}

	lapNumbers := make(map[int]int)

	for _, lap := range result.Laps {
		lapNumbers[lap.CarID]++

		sectors := lap.Sectors

		if sectors == nil {
			sectors = []int{}
		}

		out.Laps = append(out.Laps, &APILap{
			Number:     lapNumbers[lap.CarID],
			DriverName: smresults.DriverName(lap.DriverName),
			DriverGUID: lap.DriverGUID,
			CarModel:   lap.CarModel,
			LapTime:    lap.LapTime,
			Sectors:    sectors,
			Cuts:       lap.Cuts,
			Tyre:       lap.Tyre,
		})
	}

	return out
}

// listAPI returns a page of results in the API format. It takes the same page and filter query parameters as the
// results list page.
func (rh *ResultsHandler) ListAPI(w http.ResponseWriter, r *http.Request) {
	page, filter := resultsListQuery(r)

	results, pages, err := rh.env.Results().List(page, filter)

	if err == smresults.ErrPageNotFound {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result list")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	out := &APIResultsList{
		Page:    page,
		Pages:   len(pages),
		Results: make([]*APIResultSummary, 0, len(results)),
	}

	for i := range results {
		out.Results = append(out.Results, newAPIResultSummary(rh.env.InstallPath(), &results[i]))
	}

	api.WriteJSON(w, out)
}

// viewAPI returns a single results file in the API format. The file name can be given with or without extension.
func (rh *ResultsHandler) ViewAPI(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	if !strings.HasSuffix(fileName, ".json") {
		fileName += ".json"
	}

	if !smresults.ValidFileName(fileName) {
		http.NotFound(w, r)
		return
	}

	result, err := rh.env.Results().Load(fileName)

	if os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	result.ClearKickedGUIDs()
	result.NormaliseCarIDs()

	api.WriteJSON(w, NewAPIResult(rh.env.InstallPath(), result))
}
