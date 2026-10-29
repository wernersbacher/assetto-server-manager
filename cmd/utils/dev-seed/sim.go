package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

const (
	carGT3 = "dev_car_gt3"
	carMX5 = "dev_car_mx5"

	trackRing  = "dev_ring"
	trackOther = "dev_track"

	layoutGP    = "gp"
	layoutShort = "short"

	noBestLap = 999999999
)

// seedDriver is a fake driver that takes part in the seeded results, championships and races.
type seedDriver struct {
	Name   string
	GUID   string
	Nation string
	Team   string
	Class  string // "GT3" or "MX5"
	Model  string
	Skin   string
	// Pace is a lap time multiplier, lower is faster.
	Pace float64
}

var seedDrivers = []*seedDriver{
	{"Lena Hartmann", "76561198100000001", "DEU", "Apex Racing", "GT3", carGT3, "00_red", 1.000},
	{"Marco Bellini", "76561198100000002", "ITA", "Apex Racing", "GT3", carGT3, "00_red", 1.006},
	{"Jack Turner", "76561198100000003", "GBR", "Blue Fox Motorsport", "GT3", carGT3, "01_blue", 1.012},
	{"Sofia Almeida", "76561198100000004", "PRT", "Blue Fox Motorsport", "GT3", carGT3, "01_blue", 1.018},
	{"Tomasz Kowal", "76561198100000005", "POL", "", "GT3", carGT3, "00_red", 1.030},
	{"Yuki Tanabe", "76561198100000006", "JPN", "Nordschleife Nomads", "GT3", carGT3, "01_blue", 1.022},
	{"Emma Lindqvist", "76561198100000007", "SWE", "Nordschleife Nomads", "MX5", carMX5, "00_white", 1.000},
	{"Pierre Dubois", "76561198100000008", "FRA", "", "MX5", carMX5, "00_white", 1.008},
	{"Carlos Ortega", "76561198100000009", "ESP", "Blue Fox Motorsport", "MX5", carMX5, "00_white", 1.016},
	{"Sam O'Neill", "76561198100000010", "IRL", "", "MX5", carMX5, "00_white", 1.028},
}

func driverByName(name string) *seedDriver {
	for _, d := range seedDrivers {
		if strings.HasPrefix(d.Name, name) {
			return d
		}
	}

	panic("unknown seed driver " + name)
}

func driversOfClass(class string) []*seedDriver {
	var out []*seedDriver

	for _, d := range seedDrivers {
		if d.Class == class {
			out = append(out, d)
		}
	}

	return out
}

// refLapMs is the GT3 reference lap time of a track layout.
func refLapMs(track, layout string) float64 {
	switch {
	case track == trackRing && layout == layoutGP:
		return 98500
	case track == trackRing:
		return 62300
	default:
		return 85200
	}
}

type sessionSpec struct {
	Track, Layout string
	Type          serverconfig.SessionType
	Start         time.Time
	Entries       []*seedDriver
	Laps          int

	Penalties    map[string]time.Duration // by GUID
	Disqualified map[string]bool          // by GUID
	LapsDown     map[string]int           // by GUID
	Collisions   int
	CutChance    float64
}

func fileNameFor(start time.Time, t serverconfig.SessionType) string {
	return fmt.Sprintf("%d_%d_%d_%d_%d_%s", start.Year(), int(start.Month()), start.Day(), start.Hour(), start.Minute(), string(t))
}

func sessionDriver(d *seedDriver) smresults.SessionDriver {
	return smresults.SessionDriver{GUID: d.GUID, GuidsList: []string{d.GUID}, Name: d.Name, Nation: d.Nation, Team: d.Team}
}

// simulate creates the SessionResults of a session the way assetto corsa would write them.
func simulate(rng *rand.Rand, spec sessionSpec) *smresults.SessionResults {
	res := &smresults.SessionResults{
		Cars:        []*smresults.SessionCar{},
		Events:      []*smresults.SessionEvent{},
		Laps:        []*smresults.SessionLap{},
		Result:      []*smresults.SessionResult{},
		TrackConfig: spec.Layout,
		TrackName:   spec.Track,
		Type:        spec.Type,
		Date:        spec.Start,
		SessionFile: fileNameFor(spec.Start, spec.Type),
	}

	type outcome struct {
		result *smresults.SessionResult
		laps   int
		total  int
	}

	var outcomes []*outcome

	for carID, d := range spec.Entries {
		res.Cars = append(res.Cars, &smresults.SessionCar{CarID: carID, Driver: sessionDriver(d), Model: d.Model, Skin: d.Skin})

		classFactor := 1.0
		if d.Model == carMX5 {
			classFactor = 1.17
		}

		form := math.Max(-0.03, math.Min(0.03, rng.NormFloat64()*0.013))
		base := refLapMs(spec.Track, spec.Layout) * classFactor * d.Pace * (1 + form)

		numLaps := spec.Laps - spec.LapsDown[d.GUID]
		clock := float64(rng.Intn(3000))
		bestLap := noBestLap
		total := 0

		for n := 0; n < numLaps; n++ {
			lapTime := base * (1 + rng.NormFloat64()*0.003)

			switch spec.Type {
			case serverconfig.SessionTypeRace:
				if n == 0 {
					lapTime *= 1.04
				}
			case serverconfig.SessionTypePractice:
				if n == 0 {
					lapTime *= 1.30
				} else if rng.Float64() < 0.15 {
					lapTime *= 1.12
				}
			default:
				if n%3 == 0 {
					lapTime *= 1.15
				}
			}

			cuts := 0

			if rng.Float64() < spec.CutChance {
				cuts = 1
				lapTime -= 700
			}

			lap := int(math.Round(lapTime))
			s1 := int(math.Round(float64(lap) * (0.31 + rng.NormFloat64()*0.004)))
			s2 := int(math.Round(float64(lap) * (0.37 + rng.NormFloat64()*0.004)))
			clock += float64(lap)

			res.Laps = append(res.Laps, &smresults.SessionLap{
				CarID:      carID,
				CarModel:   d.Model,
				Cuts:       cuts,
				DriverGUID: d.GUID,
				DriverName: d.Name,
				LapTime:    lap,
				Sectors:    []int{s1, s2, lap - s1 - s2},
				Timestamp:  int(clock),
				Tyre:       "SM",
			})

			total += lap

			if cuts == 0 && lap < bestLap {
				bestLap = lap
			}
		}

		r := &smresults.SessionResult{
			BestLap:    bestLap,
			CarID:      carID,
			CarModel:   d.Model,
			DriverGUID: d.GUID,
			DriverName: d.Name,
			TotalTime:  total,
		}

		if penalty, ok := spec.Penalties[d.GUID]; ok {
			r.HasPenalty = true
			r.PenaltyTime = penalty
		}

		r.Disqualified = spec.Disqualified[d.GUID]

		outcomes = append(outcomes, &outcome{result: r, laps: numLaps, total: total})
	}

	sort.SliceStable(res.Laps, func(i, j int) bool { return res.Laps[i].Timestamp < res.Laps[j].Timestamp })

	sort.SliceStable(outcomes, func(i, j int) bool {
		a, b := outcomes[i], outcomes[j]

		if a.result.Disqualified != b.result.Disqualified {
			return b.result.Disqualified
		}

		if spec.Type == serverconfig.SessionTypeRace {
			if a.laps != b.laps {
				return a.laps > b.laps
			}

			return a.total+int(a.result.PenaltyTime/time.Millisecond) < b.total+int(b.result.PenaltyTime/time.Millisecond)
		}

		return a.result.BestLap < b.result.BestLap
	})

	for _, o := range outcomes {
		res.Result = append(res.Result, o.result)
	}

	for i := 0; i < spec.Collisions && len(spec.Entries) > 1; i++ {
		a := rng.Intn(len(spec.Entries))
		b := rng.Intn(len(spec.Entries))
		env := rng.Float64() < 0.35

		if a == b {
			b = (b + 1) % len(spec.Entries)
		}

		res.Events = append(res.Events, collision(rng, spec.Entries, a, b, env))

		if !env {
			res.Events = append(res.Events, collision(rng, spec.Entries, b, a, false))
		}
	}

	return res
}

func collision(rng *rand.Rand, entries []*seedDriver, a, b int, env bool) *smresults.SessionEvent {
	driver := sessionDriver(entries[a])
	other := sessionDriver(entries[b])
	otherID := b
	kind := "COLLISION_WITH_CAR"

	if env {
		other = smresults.SessionDriver{GuidsList: []string{""}}
		otherID = -1
		kind = "COLLISION_WITH_ENV"
	}

	return &smresults.SessionEvent{
		CarID:       a,
		Driver:      &driver,
		ImpactSpeed: 3 + rng.Float64()*42,
		OtherCarID:  otherID,
		OtherDriver: &other,
		RelPosition: &smresults.SessionPos{X: rng.Float64() - 0.5, Y: rng.Float64() * 0.1, Z: rng.Float64()*4 - 2},
		Type:        kind,
		WorldPosition: &smresults.SessionPos{
			X: rng.Float64()*400 - 200,
			Y: 50 + rng.Float64()*10,
			Z: rng.Float64()*400 - 200,
		},
	}
}

// writeResults writes results to the results folder of the AC install and returns the file name.
func writeResults(res *smresults.SessionResults) (string, error) {
	dir := filepath.Join(state.InstallPath(), "results")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(res, "", "\t")

	if err != nil {
		return "", err
	}

	name := res.SessionFile + ".json"

	return name, ioutil.WriteFile(filepath.Join(dir, name), data, 0644)
}

// emit simulates a session, optionally lets enhance attach championship/race weekend information to it, writes it
// to disk and loads it back the same way server manager does, so that the stored object is identical to the file.
func emit(rng *rand.Rand, spec sessionSpec, files *[]string, enhance func(*smresults.SessionResults)) (*smresults.SessionResults, error) {
	res := simulate(rng, spec)

	if enhance != nil {
		enhance(res)
	}

	name, err := writeResults(res)

	if err != nil {
		return nil, err
	}

	*files = append(*files, name)

	return (&smresults.Loader{Dir: filepath.Join(state.InstallPath(), "results")}).Load(name, smresults.WithoutPluginFire)
}
