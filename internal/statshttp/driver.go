// Package statshttp holds the handlers of the statistics pages, which need several managers (the statistics index and
// the championships).
package statshttp

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smchampionship "github.com/wernersbacher/ACPitwall/internal/championship"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/stats"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// ResultsPerPage is the number of results listed on a page of the driver profile.
const ResultsPerPage = 25

// filterServerParam restricts the results and personal bests of the profile to one server. It is not the global
// server parameter, which selects the server a page is shown for.
const filterServerParam = "filterServer"

// DriverHandler serves the driver profile page and API.
type DriverHandler struct {
	*smweb.BaseHandler

	index      *stats.Index
	servers    smweb.ServersFunc
	managerFor smchampionship.ManagerFor

	// Hide gives the filter for results of hidden championships and race weekends per request (nil: nothing is hidden).
	Hide smweb.HideFuncFor
}

// NewDriverHandler creates the handler. servers and managerFor may be nil (nothing is attributed to a server, the
// profile has no championships).
func NewDriverHandler(baseHandler *smweb.BaseHandler, index *stats.Index, servers smweb.ServersFunc, managerFor smchampionship.ManagerFor) *DriverHandler {
	return &DriverHandler{
		BaseHandler: baseHandler,
		index:       index,
		servers:     servers,
		managerFor:  managerFor,
	}
}

// ResultRow is a session of the driver for the templates, with the name of the server (only set with several servers).
type ResultRow struct {
	stats.DriverResult

	Server string
}

// RecordRow is a personal best for the templates, with the name of the server (only set with several servers).
type RecordRow struct {
	stats.Record

	Server string
}

// SessionCount is the number of sessions of a type.
type SessionCount struct {
	Type  string
	Count int
}

// ChampionshipEntry is the standing of the driver in one class of a championship.
type ChampionshipEntry struct {
	ID    string
	Name  string
	Class string // only set for championships with several classes

	// Position is the place in the standings of the class (1 is the leader), Entrants the number of drivers in it.
	Position int
	Entrants int
	Points   float64

	// Completed is true if all events of the championship are done.
	Completed bool
}

type driverTemplateVars struct {
	smweb.BaseTemplateVars

	Driver        *stats.DriverStats
	SessionCounts []SessionCount

	// ValidLapPercent is the share of valid laps, CutsPer100 the cuts per 100 laps, CollisionsPer100 the collisions
	// (with cars and the environment) per 100 laps.
	ValidLapPercent  float64
	CutsPer100       float64
	CollisionsPer100 float64

	PersonalBests []RecordRow
	Results       []ResultRow
	Championships []ChampionshipEntry

	// TotalResults is the number of results after the server filter, CurrentPage and NumPages are for Results.
	TotalResults int
	CurrentPage  int
	NumPages     int

	// FilterServer is the id of the server the lists are restricted to, empty for all.
	FilterServer string
}

// profile is what the page and the API show.
type profile struct {
	driver        *stats.DriverStats
	results       []ResultRow
	personalBests []RecordRow
	filterServer  string
	championships []ChampionshipEntry
}

// load finds the driver and applies the server filter. ok is false if the driver is unknown.
func (dh *DriverHandler) load(r *http.Request) (*profile, bool) {
	guid := chi.URLParam(r, "guid")

	if !smweb.ValidDriverGUID(guid) || dh.index == nil {
		return nil, false
	}

	driver, ok := dh.index.DriverVisible(guid, dh.Hide.For(r))

	if !ok {
		return nil, false
	}

	servers := dh.servers.Attributed()
	p := &profile{driver: driver}

	var (
		filterDir string
		filtered  bool
	)

	if id := r.URL.Query().Get(filterServerParam); id != "" && len(servers) > 0 {
		p.filterServer = id
		filtered = true
		filterDir = filepath.Join(string(filepath.Separator), "unknown-server", id)

		if dir, ok := smweb.DirOfServer(servers, id); ok {
			filterDir = dir
		}
	}

	matches := func(dir string) bool {
		return !filtered || filepath.Clean(dir) == filepath.Clean(filterDir)
	}

	serverName := func(dir string) string {
		if server := smweb.ServerOfDir(servers, dir); server != nil {
			return server.Name
		}

		return ""
	}

	for _, result := range driver.Results {
		if matches(result.Dir) {
			p.results = append(p.results, ResultRow{DriverResult: result, Server: serverName(result.Dir)})
		}
	}

	for _, record := range driver.PersonalBests {
		if matches(record.Dir) {
			p.personalBests = append(p.personalBests, RecordRow{Record: record, Server: serverName(record.Dir)})
		}
	}

	p.championships = dh.championships(r, guid)

	return p, true
}

// championships returns the standings of the driver in the championships of the server the request is for.
func (dh *DriverHandler) championships(r *http.Request, guid string) []ChampionshipEntry {
	if dh.managerFor == nil {
		return nil
	}

	cm, err := dh.managerFor(r)

	if err != nil {
		logrus.WithError(err).Error("couldn't find the championship manager of the server")
		return nil
	}

	championships, err := cm.ListChampionships()

	if err != nil {
		logrus.WithError(err).Error("couldn't list championships for the driver profile")
		return nil
	}

	visible := make([]*smmodel.Championship, 0, len(championships))

	for _, championship := range championships {
		if !championship.Hidden || smweb.CanSeeHidden(r) {
			visible = append(visible, championship)
		}
	}

	return ChampionshipEntries(visible, guid)
}

func sessionCounts(sessions map[string]int) []SessionCount {
	counts := make([]SessionCount, 0, len(sessions))

	for t, n := range sessions {
		counts = append(counts, SessionCount{Type: t, Count: n})
	}

	sort.Slice(counts, func(a, b int) bool {
		if counts[a].Count != counts[b].Count {
			return counts[a].Count > counts[b].Count
		}

		return counts[a].Type < counts[b].Type
	})

	return counts
}

func per100(n, laps int) float64 {
	if laps == 0 {
		return 0
	}

	return float64(n) * 100 / float64(laps)
}

// View renders the profile page.
func (dh *DriverHandler) View(w http.ResponseWriter, r *http.Request) {
	p, ok := dh.load(r)

	if !ok {
		http.NotFound(w, r)
		return
	}

	page, err := strconv.Atoi(r.URL.Query().Get("page"))

	if err != nil || page < 0 {
		page = 0
	}

	numPages := (len(p.results) + ResultsPerPage - 1) / ResultsPerPage

	if page > 0 && page >= numPages {
		page = numPages - 1
	}

	if page < 0 {
		page = 0
	}

	results := p.results

	if start := page * ResultsPerPage; start < len(results) {
		end := start + ResultsPerPage

		if end > len(results) {
			end = len(results)
		}

		results = results[start:end]
	}

	d := p.driver
	validPercent := 0.0

	if d.Laps > 0 {
		validPercent = float64(d.ValidLaps) * 100 / float64(d.Laps)
	}

	dh.ViewRenderer.MustLoadTemplate(w, r, "driver/view.html", &driverTemplateVars{
		Driver:           d,
		SessionCounts:    sessionCounts(d.Sessions),
		ValidLapPercent:  validPercent,
		CutsPer100:       per100(d.Cuts, d.Laps),
		CollisionsPer100: per100(d.CollisionsCar+d.CollisionsEnv, d.Laps),
		PersonalBests:    p.personalBests,
		Results:          results,
		Championships:    p.championships,
		TotalResults:     len(p.results),
		CurrentPage:      page,
		NumPages:         numPages,
		FilterServer:     p.filterServer,
	})
}

// APIPersonalBest is the best valid lap of the driver with a car on a track layout. LapTime and Sectors are in
// milliseconds.
type APIPersonalBest struct {
	Track       string
	TrackLayout string
	CarModel    string

	LapTime int
	Sectors []int
	Tyre    string

	Date        time.Time
	SessionType string

	// FileName is the results file of the session, as used by /api/results/{fileName}.
	FileName string

	// Server is the server the lap was driven on, only set if there are several servers.
	Server *api.Server `json:",omitempty"`
}

// APIResult is a session of the driver. BestLap is in milliseconds, 0 if there was no valid lap.
type APIResult struct {
	FileName    string
	Date        time.Time
	Track       string
	TrackLayout string
	SessionType string
	Cars        []string

	// Position is the finishing position in a race, 0 if disqualified or not a race.
	Position     int
	Disqualified bool
	BestLap      int
	Laps         int

	// Server is the server the session was driven on, only set if there are several servers.
	Server *api.Server `json:",omitempty"`
}

// APIChampionship is the standing of the driver in one class of a championship.
type APIChampionship struct {
	ID        string
	Name      string
	Class     string `json:",omitempty"`
	Position  int
	Entrants  int
	Points    float64
	Completed bool
}

// APIDriver is the answer of /api/driver/{guid}. The statistics cover all servers; the filterServer parameter
// restricts Results and PersonalBests.
type APIDriver struct {
	GUID   string
	Name   string
	Names  []string
	Nation string
	Team   string

	// Sessions is the number of sessions by type (as in the results, e.g. RACE, RACEx2).
	Sessions map[string]int

	Races   int
	Wins    int
	Podiums int

	Laps          int
	ValidLaps     int
	Cuts          int
	CollisionsCar int
	CollisionsEnv int

	PersonalBests []*APIPersonalBest
	Results       []*APIResult
	Championships []*APIChampionship
}

// ViewAPI answers with the profile as JSON.
func (dh *DriverHandler) ViewAPI(w http.ResponseWriter, r *http.Request) {
	p, ok := dh.load(r)

	if !ok {
		http.NotFound(w, r)
		return
	}

	servers := dh.servers.Attributed()
	serverOf := func(dir string) *api.Server {
		if server := smweb.ServerOfDir(servers, dir); server != nil {
			return &api.Server{ID: string(server.ID), Name: server.Name}
		}

		return nil
	}

	d := p.driver
	out := &APIDriver{
		GUID:          d.GUID,
		Name:          smresults.DriverName(d.Name),
		Names:         []string{},
		Nation:        d.Nation,
		Team:          d.Team,
		Sessions:      d.Sessions,
		Races:         d.Races,
		Wins:          d.Wins,
		Podiums:       d.Podiums,
		Laps:          d.Laps,
		ValidLaps:     d.ValidLaps,
		Cuts:          d.Cuts,
		CollisionsCar: d.CollisionsCar,
		CollisionsEnv: d.CollisionsEnv,

		PersonalBests: []*APIPersonalBest{},
		Results:       []*APIResult{},
		Championships: []*APIChampionship{},
	}

	for _, name := range d.Names {
		out.Names = append(out.Names, smresults.DriverName(name))
	}

	for _, record := range p.personalBests {
		sectors := make([]int, 0, len(record.Sectors))

		for _, s := range record.Sectors {
			sectors = append(sectors, int(s/time.Millisecond))
		}

		out.PersonalBests = append(out.PersonalBests, &APIPersonalBest{
			Track:       record.Track,
			TrackLayout: record.Layout,
			CarModel:    record.Car,
			LapTime:     int(record.LapTime / time.Millisecond),
			Sectors:     sectors,
			Tyre:        record.Tyre,
			Date:        record.Date,
			SessionType: record.SessionType,
			FileName:    record.ResultFile,
			Server:      serverOf(record.Dir),
		})
	}

	for _, result := range p.results {
		out.Results = append(out.Results, &APIResult{
			FileName:     result.ResultFile,
			Date:         result.Date,
			Track:        result.Track,
			TrackLayout:  result.Layout,
			SessionType:  result.SessionType,
			Cars:         result.Cars,
			Position:     result.Position,
			Disqualified: result.Disqualified,
			BestLap:      int(result.BestLap / time.Millisecond),
			Laps:         result.Laps,
			Server:       serverOf(result.Dir),
		})
	}

	for _, c := range p.championships {
		out.Championships = append(out.Championships, &APIChampionship{
			ID:        c.ID,
			Name:      c.Name,
			Class:     c.Class,
			Position:  c.Position,
			Entrants:  c.Entrants,
			Points:    c.Points,
			Completed: c.Completed,
		})
	}

	w.Header().Add("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(out); err != nil {
		logrus.WithError(err).Error("couldn't encode the driver profile")
	}
}
