package contenthttp

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/stats"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// ServerDir is a server slot together with its results directory.
type ServerDir = smweb.ServerDir

// ServersFunc returns all server slots with their results directories (see smweb.ServersFunc). Records are attributed
// to a server by the directory of their results file.
type ServersFunc = smweb.ServersFunc

// filterServerParam restricts the records to one server. It is not the global server parameter, which selects the
// server a page is shown for.
const filterServerParam = "filterServer"

// Records reads lap records from the statistics index for the track and car pages and the records API.
type Records struct {
	index   *stats.Index
	servers ServersFunc

	// Hide gives the filter for results of hidden championships and race weekends per request (nil: nothing is
	// hidden). The resolver sets it.
	Hide smweb.HideFuncFor
}

// NewRecords returns the records source. servers may be nil (nothing is attributed to a server).
func NewRecords(index *stats.Index, servers ServersFunc) *Records {
	return &Records{index: index, servers: servers}
}

// RecordRow is a record for the templates, with the name of the server it was driven on (only set with several servers).
type RecordRow struct {
	stats.Record

	Server string
}

// attributedServers returns the servers for attributing records: none if there are fewer than two.
func (rs *Records) attributedServers() []ServerDir {
	if rs == nil {
		return nil
	}

	return rs.servers.Attributed()
}

// filter reads filterServer from the request. It returns the filter and the server id as given. An unknown id matches
// nothing.
func (rs *Records) filter(r *http.Request) (stats.Filter, string) {
	filter := stats.Filter{Hide: rs.hideFunc(r)}

	id := r.URL.Query().Get(filterServerParam)
	servers := rs.attributedServers()

	if id == "" || len(servers) == 0 {
		return filter, ""
	}

	filter.Dir = filepath.Join(string(filepath.Separator), "unknown-server", id)

	if dir, ok := smweb.DirOfServer(servers, id); ok {
		filter.Dir = dir
	}

	return filter, id
}

func (rs *Records) rows(records []stats.Record) []RecordRow {
	servers := rs.attributedServers()
	rows := make([]RecordRow, 0, len(records))

	for _, record := range records {
		row := RecordRow{Record: record}

		if server := smweb.ServerOfDir(servers, record.Dir); server != nil {
			row.Server = server.Name
		}

		rows = append(rows, row)
	}

	return rows
}

// forTrack returns the best lap per car on the track layout, fastest first.
func (rs *Records) forTrack(track, layout string, filter stats.Filter) []stats.Record {
	if rs == nil || rs.index == nil {
		return nil
	}

	return rs.index.TrackRecords(track, layout, filter)
}

// forCar returns the best lap of the car per track layout.
func (rs *Records) forCar(car string, filter stats.Filter) []stats.Record {
	if rs == nil || rs.index == nil {
		return nil
	}

	return rs.index.CarRecords(car, filter)
}

// APIRecord is a lap record in the API format. LapTime and Sectors are in milliseconds.
type APIRecord struct {
	DriverName string
	DriverGUID string

	CarModel string
	CarName  string

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

// APITrackRecords is the answer of /api/track-records.
type APITrackRecords struct {
	Track       string
	TrackLayout string
	TrackName   string

	// Overall is the fastest record of all returned records, nil if there are none.
	Overall *APIRecord

	// Records is the best valid lap of every car, fastest first.
	Records []*APIRecord
}

func newAPIRecord(rs *Records, servers []ServerDir, record stats.Record, prettyCar string) *APIRecord {
	sectors := make([]int, 0, len(record.Sectors))

	for _, s := range record.Sectors {
		sectors = append(sectors, int(s/time.Millisecond))
	}

	out := &APIRecord{
		DriverName:  smresults.DriverName(record.DriverName),
		DriverGUID:  record.DriverGUID,
		CarModel:    record.Car,
		CarName:     prettyCar,
		LapTime:     int(record.LapTime / time.Millisecond),
		Sectors:     sectors,
		Tyre:        record.Tyre,
		Date:        record.Date,
		SessionType: record.SessionType,
		FileName:    record.ResultFile,
	}

	if server := smweb.ServerOfDir(servers, record.Dir); server != nil {
		out.Server = &api.Server{ID: string(server.ID), Name: server.Name}
	}

	return out
}

// hideFunc is the filter for results of hidden championships and race weekends for the request.
func (rs *Records) hideFunc(r *http.Request) smresults.HideFunc {
	if rs == nil {
		return nil
	}

	return rs.Hide.For(r)
}
