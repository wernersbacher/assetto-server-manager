package contenthttp

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/wernersbacher/assetto-server-manager/internal/api"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/internal/stats"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// ServerDir is a server slot together with its results directory.
type ServerDir struct {
	smweb.ServerRef

	ResultsDir string
}

// ServersFunc returns all server slots with their results directories. The resolver provides it, as the handlers
// cannot know the slots. Records are attributed to a server by the directory of their results file.
type ServersFunc func() []ServerDir

// filterServerParam restricts the records to one server. It is not the global server parameter, which selects the
// server a page is shown for.
const filterServerParam = "filterServer"

// Records reads lap records from the statistics index for the track and car pages and the records API.
type Records struct {
	index   *stats.Index
	servers ServersFunc
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
	if rs == nil || rs.servers == nil {
		return nil
	}

	servers := rs.servers()

	if len(servers) < 2 {
		return nil
	}

	return servers
}

// filter reads filterServer from the request. It returns the filter and the server id as given. An unknown id matches
// nothing.
func (rs *Records) filter(r *http.Request) (stats.Filter, string) {
	var filter stats.Filter

	id := r.URL.Query().Get(filterServerParam)
	servers := rs.attributedServers()

	if id == "" || len(servers) == 0 {
		return filter, ""
	}

	filter.Dir = filepath.Join(string(filepath.Separator), "unknown-server", id)

	for _, server := range servers {
		if string(server.ID) == id {
			filter.Dir = server.ResultsDir
		}
	}

	return filter, id
}

func (rs *Records) serverOf(servers []ServerDir, dir string) *smweb.ServerRef {
	for _, server := range servers {
		if filepath.Clean(server.ResultsDir) == filepath.Clean(dir) {
			ref := server.ServerRef

			return &ref
		}
	}

	return nil
}

func (rs *Records) rows(records []stats.Record) []RecordRow {
	servers := rs.attributedServers()
	rows := make([]RecordRow, 0, len(records))

	for _, record := range records {
		row := RecordRow{Record: record}

		if server := rs.serverOf(servers, record.Dir); server != nil {
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

	if server := rs.serverOf(servers, record.Dir); server != nil {
		out.Server = &api.Server{ID: string(server.ID), Name: server.Name}
	}

	return out
}
