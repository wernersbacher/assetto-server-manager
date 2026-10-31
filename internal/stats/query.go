package stats

import (
	"sort"
	"strings"
	"time"
)

// Filter narrows down the laps and sessions a query looks at. Empty fields do not filter.
type Filter struct {
	Cars         []string  // car models
	Since        time.Time // sessions from this date on
	SessionTypes []string  // e.g. "practice", "qualify", "race" (case-insensitive; "race" includes RACEx2)
	Dir          string    // results directory (server slot), as reported in Record.Dir

	// PerCar makes Leaderboard list the best lap of every driver and car instead of one per driver.
	PerCar bool
}

func (f Filter) matchesCar(model string) bool {
	if len(f.Cars) == 0 {
		return true
	}

	for _, c := range f.Cars {
		if c == model {
			return true
		}
	}

	return false
}

func (f Filter) matchesFile(s *fileStats) bool {
	return (f.Dir == "" || cleanDir(f.Dir) == cleanDir(s.dir)) &&
		(f.Since.IsZero() || !s.date.Before(f.Since)) &&
		sessionTypeMatches(s.sessionType, f.SessionTypes)
}

func cleanDir(dir string) string {
	return strings.TrimRight(strings.Replace(dir, "\\", "/", -1), "/")
}

// Record is the best valid lap of a driver with a car in one session.
type Record struct {
	DriverGUID  string
	DriverName  string
	Car         string
	Track       string
	Layout      string
	LapTime     time.Duration
	Sectors     []time.Duration
	Tyre        string
	Timestamp   int // the lap's timestamp as stored in the results file
	Date        time.Time
	ResultFile  string // file name of the session, with extension
	Dir         string
	SessionType string
}

// betterThan orders records: faster first, on equal times the older one (it set the record first).
func (r *Record) betterThan(o *Record) bool {
	if r.LapTime != o.LapTime {
		return r.LapTime < o.LapTime
	}

	return r.Date.Before(o.Date)
}

func sortRecords(records []Record) {
	sort.SliceStable(records, func(a, b int) bool {
		if records[a].betterThan(&records[b]) {
			return true
		}

		if records[b].betterThan(&records[a]) {
			return false
		}

		return records[a].DriverGUID+records[a].Car < records[b].DriverGUID+records[b].Car
	})
}

func (f *fileStats) record(d *fileDriver, car string, c *carStats) Record {
	name := c.best.name

	if name == "" {
		name = d.name()
	}

	return Record{
		DriverGUID:  d.guid,
		DriverName:  name,
		Car:         car,
		Track:       f.track,
		Layout:      f.layout,
		LapTime:     c.best.time,
		Sectors:     c.best.sectors,
		Tyre:        c.best.tyre,
		Timestamp:   c.best.timestamp,
		Date:        f.date,
		ResultFile:  f.name,
		Dir:         f.dir,
		SessionType: f.sessionType,
	}
}

// bests collects the best valid lap of every key (as given by key) over the matching sessions.
func (i *Index) bests(track, layout string, f Filter, key func(r *Record) string) []Record {
	best := make(map[string]Record)

	for _, s := range i.snapshot() {
		if !strings.EqualFold(s.track, track) || !strings.EqualFold(s.layout, layout) || !f.matchesFile(s) {
			continue
		}

		for _, d := range s.drivers {
			for car, c := range d.cars {
				if c.best == nil || !f.matchesCar(car) {
					continue
				}

				r := s.record(d, car, c)
				k := key(&r)

				if cur, ok := best[k]; !ok || r.betterThan(&cur) {
					best[k] = r
				}
			}
		}
	}

	records := make([]Record, 0, len(best))

	for _, r := range best {
		records = append(records, r)
	}

	sortRecords(records)

	return records
}

// TrackRecords returns the best valid lap of every car on the track and layout, fastest first: the first entry is the
// overall record. Track and layout are compared case-insensitively; an empty layout is the default layout.
func (i *Index) TrackRecords(track, layout string, f Filter) []Record {
	return i.bests(track, layout, f, func(r *Record) string { return r.Car })
}

// Leaderboard returns the best valid lap of every driver on the track and layout, fastest first (of every driver and
// car with Filter.PerCar).
func (i *Index) Leaderboard(track, layout string, f Filter) []Record {
	if f.PerCar {
		return i.bests(track, layout, f, func(r *Record) string { return r.DriverGUID + "\x00" + r.Car })
	}

	return i.bests(track, layout, f, func(r *Record) string { return r.DriverGUID })
}

// CarRecords returns the best valid lap of the car on every track and layout, ordered by track and layout.
func (i *Index) CarRecords(car string, f Filter) []Record {
	best := make(map[string]Record)

	for _, s := range i.snapshot() {
		if !f.matchesFile(s) {
			continue
		}

		for _, d := range s.drivers {
			if c, ok := d.cars[car]; ok && c.best != nil {
				r := s.record(d, car, c)
				k := strings.ToLower(r.Track) + "\x00" + strings.ToLower(r.Layout)

				if cur, ok := best[k]; !ok || r.betterThan(&cur) {
					best[k] = r
				}
			}
		}
	}

	return sortedByTrack(best)
}

// TrackRef identifies a track layout.
type TrackRef struct {
	Track  string
	Layout string
}

// TrackSummary is a track layout with valid laps and the date of the newest session with one.
type TrackSummary struct {
	TrackRef
	Latest time.Time
}

// Tracks lists the tracks and layouts with at least one valid lap, ordered by name.
func (i *Index) Tracks() []TrackRef {
	summaries := i.TrackSummaries(Filter{})
	refs := make([]TrackRef, 0, len(summaries))

	for _, s := range summaries {
		refs = append(refs, s.TrackRef)
	}

	return refs
}

// TrackSummaries lists the tracks and layouts with at least one valid lap in the sessions matching the filter (Dir,
// Since and SessionTypes; Cars is ignored), ordered by name, with the date of their newest session. It scans the index
// once, unlike calling TrackRecords per track.
func (i *Index) TrackSummaries(f Filter) []TrackSummary {
	f.Cars = nil
	seen := make(map[TrackRef]time.Time)

	for _, s := range i.snapshot() {
		if !f.matchesFile(s) || !s.hasValidLap() {
			continue
		}

		ref := TrackRef{s.track, s.layout}

		if latest, ok := seen[ref]; !ok || s.date.After(latest) {
			seen[ref] = s.date
		}
	}

	out := make([]TrackSummary, 0, len(seen))

	for ref, latest := range seen {
		out = append(out, TrackSummary{TrackRef: ref, Latest: latest})
	}

	sort.Slice(out, func(a, b int) bool {
		if out[a].Track != out[b].Track {
			return out[a].Track < out[b].Track
		}

		return out[a].Layout < out[b].Layout
	})

	return out
}

func sortedByTrack(m map[string]Record) []Record {
	records := make([]Record, 0, len(m))

	for _, r := range m {
		records = append(records, r)
	}

	sort.Slice(records, func(a, b int) bool {
		if records[a].Track != records[b].Track {
			return records[a].Track < records[b].Track
		}

		return records[a].Layout < records[b].Layout
	})

	return records
}

// DriverResult is the participation of a driver in one session.
type DriverResult struct {
	ResultFile   string
	Dir          string
	Date         time.Time
	Track        string
	Layout       string
	SessionType  string
	Cars         []string // cars driven, most laps first
	Position     int      // finishing position in a race, 0 if disqualified or not a race
	Disqualified bool
	BestLap      time.Duration // best valid lap, 0 if there was none
	Laps         int
}

// DriverStats is the profile of a driver.
type DriverStats struct {
	GUID   string
	Name   string   // the name used last
	Names  []string // all names used, the one used last first
	Nation string   // of the newest session that has one
	Team   string   // of the newest session that has one

	Sessions map[string]int // number of sessions by type (as in the results, e.g. RACE, RACEx2)

	Races   int
	Wins    int
	Podiums int

	Laps          int
	ValidLaps     int
	Cuts          int
	CollisionsCar int
	CollisionsEnv int

	Results       []DriverResult // newest first
	PersonalBests []Record       // best valid lap per track, layout and car, ordered by track and layout
}

// Driver returns the statistics of the driver with the GUID. ok is false if the driver is not in any results.
func (i *Index) Driver(guid string) (*DriverStats, bool) {
	if guid == "" {
		return nil, false
	}

	var files []*fileStats

	for _, s := range i.snapshot() {
		if _, ok := s.drivers[guid]; ok {
			files = append(files, s)
		}
	}

	if len(files) == 0 {
		return nil, false
	}

	// oldest first, so that "used last" is the end
	sort.SliceStable(files, func(a, b int) bool {
		if !files[a].date.Equal(files[b].date) {
			return files[a].date.Before(files[b].date)
		}

		return files[a].dir+files[a].name < files[b].dir+files[b].name
	})

	stats := &DriverStats{GUID: guid, Sessions: make(map[string]int)}
	pbs := make(map[string]Record)

	for _, s := range files {
		d := s.drivers[guid]

		for _, name := range d.names {
			stats.Names = moveToEnd(stats.Names, name)
		}

		if d.nation != "" {
			stats.Nation = d.nation
		}

		if d.team != "" {
			stats.Team = d.team
		}

		res := DriverResult{
			ResultFile:   s.name,
			Dir:          s.dir,
			Date:         s.date,
			Track:        s.track,
			Layout:       s.layout,
			SessionType:  s.sessionType,
			Position:     d.position,
			Disqualified: d.disqualified,
		}

		type carLaps struct {
			car  string
			laps int
		}

		var cars []carLaps

		for car, c := range d.cars {
			stats.Laps += c.laps
			stats.ValidLaps += c.validLaps
			stats.Cuts += c.cuts
			stats.CollisionsCar += c.collisionsCar
			stats.CollisionsEnv += c.collisionsEnv
			res.Laps += c.laps

			if c.laps > 0 || c.best != nil {
				cars = append(cars, carLaps{car, c.laps})
			}

			if c.best == nil {
				continue
			}

			r := s.record(d, car, c)

			if res.BestLap == 0 || r.LapTime < res.BestLap {
				res.BestLap = r.LapTime
			}

			k := strings.ToLower(s.track) + "\x00" + strings.ToLower(s.layout) + "\x00" + car

			if cur, ok := pbs[k]; !ok || r.betterThan(&cur) {
				pbs[k] = r
			}
		}

		sort.Slice(cars, func(a, b int) bool {
			if cars[a].laps != cars[b].laps {
				return cars[a].laps > cars[b].laps
			}

			return cars[a].car < cars[b].car
		})

		for _, c := range cars {
			res.Cars = append(res.Cars, c.car)
		}

		stats.Sessions[s.sessionType]++

		if isRace(s.sessionType) && (d.hasResult || res.Laps > 0) {
			stats.Races++

			if d.position == 1 {
				stats.Wins++
			}

			if d.position >= 1 && d.position <= 3 {
				stats.Podiums++
			}
		}

		stats.Results = append(stats.Results, res)
	}

	// newest first
	for a, b := 0, len(stats.Results)-1; a < b; a, b = a+1, b-1 {
		stats.Results[a], stats.Results[b] = stats.Results[b], stats.Results[a]
	}

	for a, b := 0, len(stats.Names)-1; a < b; a, b = a+1, b-1 {
		stats.Names[a], stats.Names[b] = stats.Names[b], stats.Names[a]
	}

	if len(stats.Names) > 0 {
		stats.Name = stats.Names[0]
	}

	for _, r := range pbs {
		stats.PersonalBests = append(stats.PersonalBests, r)
	}

	sort.Slice(stats.PersonalBests, func(a, b int) bool {
		x, y := stats.PersonalBests[a], stats.PersonalBests[b]

		if x.Track != y.Track {
			return x.Track < y.Track
		}

		if x.Layout != y.Layout {
			return x.Layout < y.Layout
		}

		return x.Car < y.Car
	})

	return stats, true
}
