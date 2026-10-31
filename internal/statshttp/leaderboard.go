package statshttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	"github.com/wernersbacher/ACPitwall/internal/plainname"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/stats"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// Periods the leaderboard can be restricted to.
const (
	PeriodAll    = "all"
	Period30Days = "30d"
	Period7Days  = "7d"
)

// sessionTypes are the values of the sessionType parameter.
var sessionTypes = []string{"practice", "qualify", "race"}

// LeaderboardHandler serves the hotlap leaderboard page and API.
type LeaderboardHandler struct {
	*smweb.BaseHandler

	index       *stats.Index
	servers     smweb.ServersFunc
	installPath func() string

	// Hide gives the filter for results of hidden championships and race weekends per request (nil: nothing is hidden).
	Hide smweb.HideFuncFor

	// now is the clock, replaced in tests.
	now func() time.Time
}

// NewLeaderboardHandler creates the handler. servers may be nil (nothing is attributed to a server).
func NewLeaderboardHandler(baseHandler *smweb.BaseHandler, index *stats.Index, servers smweb.ServersFunc, installPath func() string) *LeaderboardHandler {
	return &LeaderboardHandler{
		BaseHandler: baseHandler,
		index:       index,
		servers:     servers,
		installPath: installPath,
		now:         time.Now,
	}
}

// query is a validated leaderboard request.
type query struct {
	track, layout string
	cars          []string
	period        string
	sessionType   string
	perCar        bool
	filterServer  string
	dir           string // results dir of filterServer
	hide          smresults.HideFunc
}

// parse validates the request parameters. The error text is for the client.
func (lh *LeaderboardHandler) parse(r *http.Request) (*query, error) {
	values := r.URL.Query()
	q := &query{
		track:       values.Get("track"),
		layout:      values.Get("layout"),
		period:      values.Get("period"),
		sessionType: strings.ToLower(values.Get("sessionType")),
		hide:        lh.Hide.For(r),
	}

	// the page form submits track and layout as one select value
	if tl := values.Get("trackLayout"); tl != "" {
		parts := strings.SplitN(tl, "|", 2)
		q.track = parts[0]
		q.layout = ""

		if len(parts) == 2 {
			q.layout = parts[1]
		}
	}

	if (q.track != "" && !plainname.Valid(q.track)) || (q.layout != "" && !plainname.Valid(q.layout)) {
		return nil, fmt.Errorf("track and layout must be valid names")
	}

	for _, car := range values["car"] {
		if car == "" {
			continue
		}

		if !plainname.Valid(car) {
			return nil, fmt.Errorf("car must be a valid name")
		}

		q.cars = append(q.cars, car)
	}

	switch q.period {
	case "", PeriodAll:
		q.period = PeriodAll
	case Period30Days, Period7Days:
	default:
		return nil, fmt.Errorf("period must be one of all, 30d, 7d")
	}

	if q.sessionType != "" {
		valid := false

		for _, t := range sessionTypes {
			valid = valid || t == q.sessionType
		}

		if !valid {
			return nil, fmt.Errorf("sessionType must be one of practice, qualify, race")
		}
	}

	switch values.Get("perCar") {
	case "", "0", "false":
	case "1", "true", "on":
		q.perCar = true
	default:
		return nil, fmt.Errorf("perCar must be 1 or 0")
	}

	servers := lh.servers.Attributed()

	if id := values.Get(filterServerParam); id != "" && len(servers) > 0 {
		q.filterServer = id
		q.dir = filepath.Join(string(filepath.Separator), "unknown-server", id)

		if dir, ok := smweb.DirOfServer(servers, id); ok {
			q.dir = dir
		}
	}

	return q, nil
}

// filter is the index filter without the cars.
func (lh *LeaderboardHandler) filter(q *query) stats.Filter {
	f := stats.Filter{Dir: q.dir, Hide: q.hide}

	switch q.period {
	case Period30Days:
		f.Since = lh.now().AddDate(0, 0, -30)
	case Period7Days:
		f.Since = lh.now().AddDate(0, 0, -7)
	}

	if q.sessionType != "" {
		f.SessionTypes = []string{q.sessionType}
	}

	return f
}

// TrackOption is an entry of the track select.
type TrackOption struct {
	Track  string
	Layout string
	Name   string
	Value  string // track and layout as one select value
	Latest time.Time
}

// CarOption is an entry of the car list.
type CarOption struct {
	Model    string
	Name     string
	Selected bool
}

// LeaderboardRow is a place on the leaderboard.
type LeaderboardRow struct {
	stats.Record

	Position int

	// Gap is the distance to the leader, GapText its display form ("+1.234"), empty for the leader.
	Gap     time.Duration
	GapText string

	// BestSectors tells for each sector if it is the best of its column.
	BestSectors []bool

	// Server is the name of the server (only set with several servers).
	Server string
}

func formatGap(d time.Duration) string {
	if d <= 0 {
		return ""
	}

	if d >= time.Minute {
		return fmt.Sprintf("+%d:%06.3f", int(d/time.Minute), (d % time.Minute).Seconds())
	}

	return fmt.Sprintf("+%.3f", d.Seconds())
}

// rows numbers the records and computes gaps and best sectors.
func (lh *LeaderboardHandler) rows(records []stats.Record) []LeaderboardRow {
	servers := lh.servers.Attributed()
	rows := make([]LeaderboardRow, 0, len(records))

	var bestSector []time.Duration

	for _, record := range records {
		for i, s := range record.Sectors {
			for len(bestSector) <= i {
				bestSector = append(bestSector, 0)
			}

			if s > 0 && (bestSector[i] == 0 || s < bestSector[i]) {
				bestSector[i] = s
			}
		}
	}

	for i, record := range records {
		row := LeaderboardRow{Record: record, Position: i + 1}

		if i > 0 {
			row.Gap = record.LapTime - records[0].LapTime
			row.GapText = formatGap(row.Gap)
		}

		for j, s := range record.Sectors {
			row.BestSectors = append(row.BestSectors, s > 0 && s == bestSector[j])
		}

		if server := smweb.ServerOfDir(servers, record.Dir); server != nil {
			row.Server = server.Name
		}

		rows = append(rows, row)
	}

	return rows
}

// trackOptions lists the tracks with laps under the filter (without cars) by name, and the selected one: the one asked
// for or, if none was, the one with the newest lap.
func (lh *LeaderboardHandler) trackOptions(f stats.Filter, track, layout string) (options []TrackOption, selected *TrackOption) {
	if lh.index == nil {
		return nil, nil
	}

	for _, ref := range lh.index.TrackSummaries(stats.Filter{Dir: f.Dir, Hide: f.Hide}) {
		options = append(options, TrackOption{
			Track:  ref.Track,
			Layout: ref.Layout,
			Name:   api.TrackName(lh.installPath(), ref.Track, ref.Layout),
			Value:  ref.Track + "|" + ref.Layout,
			Latest: ref.Latest,
		})
	}

	sort.SliceStable(options, func(a, b int) bool {
		return strings.ToLower(options[a].Name) < strings.ToLower(options[b].Name)
	})

	for i := range options {
		if track != "" {
			if strings.EqualFold(options[i].Track, track) && strings.EqualFold(options[i].Layout, layout) {
				return options, &options[i]
			}
		} else if selected == nil || options[i].Latest.After(selected.Latest) {
			selected = &options[i]
		}
	}

	return options, selected
}

// leaderboard is what the page shows.
type leaderboard struct {
	tracks  []TrackOption
	track   string
	layout  string
	cars    []CarOption
	records []stats.Record
}

func (lh *LeaderboardHandler) load(q *query) *leaderboard {
	lb := &leaderboard{track: q.track, layout: q.layout}
	f := lh.filter(q)

	var selected *TrackOption

	lb.tracks, selected = lh.trackOptions(f, q.track, q.layout)

	if selected != nil {
		lb.track, lb.layout = selected.Track, selected.Layout
	}

	if lh.index == nil || lb.track == "" {
		return lb
	}

	// the cars with laps under the other filters
	chosen := make(map[string]bool)

	for _, car := range q.cars {
		chosen[car] = true
	}

	for _, record := range lh.index.TrackRecords(lb.track, lb.layout, f) {
		lb.cars = append(lb.cars, CarOption{
			Model:    record.Car,
			Name:     smcontent.PrettifyName(record.Car, true),
			Selected: chosen[record.Car],
		})
	}

	sort.Slice(lb.cars, func(a, b int) bool { return lb.cars[a].Name < lb.cars[b].Name })

	f.Cars = q.cars
	f.PerCar = q.perCar
	lb.records = lh.index.Leaderboard(lb.track, lb.layout, f)

	return lb
}

type leaderboardTemplateVars struct {
	smweb.BaseTemplateVars

	Tracks    []TrackOption
	Track     string
	Layout    string
	TrackName string
	Value     string // the selected value of the track select

	Cars []CarOption

	Period       string
	SessionType  string
	PerCar       bool
	FilterServer string

	// Filtered is true if anything but the track is restricted.
	Filtered bool

	Rows []LeaderboardRow

	// SectorCount is the number of sector columns.
	SectorCount int
}

// View renders the leaderboard page.
func (lh *LeaderboardHandler) View(w http.ResponseWriter, r *http.Request) {
	q, err := lh.parse(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	lb := lh.load(q)
	rows := lh.rows(lb.records)

	vars := &leaderboardTemplateVars{
		Tracks:       lb.tracks,
		Track:        lb.track,
		Layout:       lb.layout,
		Cars:         lb.cars,
		Period:       q.period,
		SessionType:  q.sessionType,
		PerCar:       q.perCar,
		FilterServer: q.filterServer,
		Filtered:     len(q.cars) > 0 || q.period != PeriodAll || q.sessionType != "" || q.perCar || q.filterServer != "",
		Rows:         rows,
	}

	if lb.track != "" {
		vars.TrackName = api.TrackName(lh.installPath(), lb.track, lb.layout)
		vars.Value = lb.track + "|" + lb.layout
	}

	for _, row := range rows {
		if len(row.Sectors) > vars.SectorCount {
			vars.SectorCount = len(row.Sectors)
		}
	}

	lh.ViewRenderer.MustLoadTemplate(w, r, "leaderboards/index.html", vars)
}

// APILeaderboardEntry is a place on the leaderboard. LapTime, Gap and Sectors are in milliseconds.
type APILeaderboardEntry struct {
	Position int

	DriverName string
	DriverGUID string

	CarModel string
	CarName  string

	LapTime int
	Gap     int
	Sectors []int
	Tyre    string

	Date        time.Time
	SessionType string

	// FileName is the results file of the session, as used by /api/results/{fileName}.
	FileName string

	// Server is the server the lap was driven on, only set if there are several servers.
	Server *api.Server `json:",omitempty"`
}

// APILeaderboard is the answer of /api/leaderboard.
type APILeaderboard struct {
	Track       string
	TrackLayout string
	TrackName   string

	Period      string
	SessionType string
	PerCar      bool
	Cars        []string

	Entries []*APILeaderboardEntry
}

// ViewAPI answers with the leaderboard as JSON. The track is required.
func (lh *LeaderboardHandler) ViewAPI(w http.ResponseWriter, r *http.Request) {
	q, err := lh.parse(r)

	if err == nil && q.track == "" {
		err = fmt.Errorf("track is required")
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f := lh.filter(q)
	f.Cars = q.cars
	f.PerCar = q.perCar

	var records []stats.Record

	if lh.index != nil {
		records = lh.index.Leaderboard(q.track, q.layout, f)
	}

	servers := lh.servers.Attributed()
	out := &APILeaderboard{
		Track:       q.track,
		TrackLayout: q.layout,
		TrackName:   api.TrackName(lh.installPath(), q.track, q.layout),
		Period:      q.period,
		SessionType: q.sessionType,
		PerCar:      q.perCar,
		Cars:        q.cars,
		Entries:     []*APILeaderboardEntry{},
	}

	if out.Cars == nil {
		out.Cars = []string{}
	}

	for _, row := range lh.rows(records) {
		sectors := make([]int, 0, len(row.Sectors))

		for _, s := range row.Sectors {
			sectors = append(sectors, int(s/time.Millisecond))
		}

		entry := &APILeaderboardEntry{
			Position:    row.Position,
			DriverName:  smresults.DriverName(row.DriverName),
			DriverGUID:  row.DriverGUID,
			CarModel:    row.Car,
			CarName:     smcontent.PrettifyName(row.Car, true),
			LapTime:     int(row.LapTime / time.Millisecond),
			Gap:         int(row.Gap / time.Millisecond),
			Sectors:     sectors,
			Tyre:        row.Tyre,
			Date:        row.Date,
			SessionType: row.SessionType,
			FileName:    row.ResultFile,
		}

		if server := smweb.ServerOfDir(servers, row.Dir); server != nil {
			entry.Server = &api.Server{ID: string(server.ID), Name: server.Name}
		}

		out.Entries = append(out.Entries, entry)
	}

	w.Header().Add("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(out); err != nil {
		logrus.WithError(err).Error("couldn't encode the leaderboard")
	}
}
