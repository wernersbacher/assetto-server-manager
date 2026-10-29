package results

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

// GUIDs must be numeric, NormaliseEntrantGUIDs strips everything that isn't a digit.
const (
	helperGUIDAlice = "1001"
	helperGUIDBob   = "1002"
	helperGUIDCarl1 = "1003"
	helperGUIDCarl2 = "1004"
	helperGUIDDan   = "1005"
	helperGUIDSwap  = "1003;1004" // normalised GUID of the driver swap car

	helperModelFerrari = "ferrari"
	helperModelPorsche = "porsche"
)

var (
	helperClassFerrari = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	helperClassPorsche = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

func helperLap(carID int, guid, name, model string, lapTime, cuts int, sectors []int, tyre string, classID uuid.UUID) *SessionLap {
	return &SessionLap{
		CarID:      carID,
		CarModel:   model,
		Cuts:       cuts,
		DriverGUID: guid,
		DriverName: name,
		LapTime:    lapTime,
		Sectors:    sectors,
		Tyre:       tyre,
		ClassID:    classID,
	}
}

// newHelperResults builds a small race:
//
//	car 1: Alice (ferrari, team "Red"), 3 clean laps
//	car 2: Bob (ferrari), 3 laps, the fastest of them cut, 5s time penalty
//	car 3: driver swap Carl1/Carl2 (porsche), 3 laps: Carl1, Carl2, Carl1
//	car 4: Dan (ferrari), no laps at all
//
// Car IDs start at 1 on purpose, so that a lookup of an unknown driver can't accidentally match car 0
// (see newHelperResultsWithCarZero for that case).
func newHelperResults() *SessionResults {
	return &SessionResults{
		Type: serverconfig.SessionTypeRace,
		Cars: []*SessionCar{
			{CarID: 1, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDAlice, GuidsList: []string{helperGUIDAlice}, Name: "Alice Smith", Team: "Red", ClassID: helperClassFerrari}},
			{CarID: 2, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDBob, GuidsList: []string{helperGUIDBob}, Name: "Bob Jones", ClassID: helperClassFerrari}},
			{CarID: 3, Model: helperModelPorsche, Driver: SessionDriver{GUID: helperGUIDCarl1, GuidsList: []string{helperGUIDCarl1, helperGUIDCarl2}, Name: "Carl One", ClassID: helperClassPorsche}},
			{CarID: 4, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDDan, GuidsList: []string{helperGUIDDan}, Name: "Dan Nolaps", ClassID: helperClassFerrari}},
		},
		Laps: []*SessionLap{
			helperLap(1, helperGUIDAlice, "Alice Smith", helperModelFerrari, 100000, 0, []int{30000, 40000, 30000}, "M", helperClassFerrari),
			helperLap(2, helperGUIDBob, "Bob Jones", helperModelFerrari, 102000, 0, []int{31000, 40000, 31000}, "M", helperClassFerrari),
			helperLap(3, helperGUIDCarl1, "Carl One", helperModelPorsche, 105000, 0, []int{35000, 35000, 35000}, "H", helperClassPorsche),
			helperLap(1, helperGUIDAlice, "Alice Smith", helperModelFerrari, 98000, 0, []int{29000, 39000, 30000}, "S", helperClassFerrari),
			helperLap(2, helperGUIDBob, "Bob Jones", helperModelFerrari, 95000, 2, []int{28000, 38000, 29000}, "S", helperClassFerrari),
			helperLap(3, helperGUIDCarl2, "Carl Two", helperModelPorsche, 99000, 0, []int{33000, 36000, 30000}, "H", helperClassPorsche),
			helperLap(1, helperGUIDAlice, "Alice Smith", helperModelFerrari, 99000, 0, []int{30000, 38500, 30500}, "S", helperClassFerrari),
			helperLap(2, helperGUIDBob, "Bob Jones", helperModelFerrari, 101000, 0, []int{30500, 39500, 31000}, "M", helperClassFerrari),
			helperLap(3, helperGUIDCarl1, "Carl One", helperModelPorsche, 104000, 0, []int{34000, 36000, 34000}, "H", helperClassPorsche),
		},
		Result: []*SessionResult{
			{CarID: 1, CarModel: helperModelFerrari, DriverGUID: helperGUIDAlice, DriverName: "Alice Smith", BestLap: 98000, TotalTime: 297000, ClassID: helperClassFerrari},
			{CarID: 2, CarModel: helperModelFerrari, DriverGUID: helperGUIDBob, DriverName: "Bob Jones", BestLap: 101000, TotalTime: 298000, HasPenalty: true, PenaltyTime: 5 * time.Second, ClassID: helperClassFerrari},
			{CarID: 3, CarModel: helperModelPorsche, DriverGUID: helperGUIDSwap, DriverName: "Carl One", BestLap: 99000, TotalTime: 308000, ClassID: helperClassPorsche},
			{CarID: 4, CarModel: helperModelFerrari, DriverGUID: helperGUIDDan, DriverName: "Dan Nolaps", ClassID: helperClassFerrari},
		},
		Events: []*SessionEvent{
			{CarID: 1, OtherCarID: 2, Type: "COLLISION_WITH_CAR", Driver: &SessionDriver{GUID: helperGUIDAlice, Name: "Alice Smith"}, OtherDriver: &SessionDriver{GUID: helperGUIDBob, Name: "Bob Jones"}},
			{CarID: 1, Type: "COLLISION_WITH_ENV", Driver: &SessionDriver{GUID: helperGUIDAlice, Name: "Alice Smith"}, OtherDriver: &SessionDriver{}},
			{CarID: 3, Type: "COLLISION_WITH_ENV", Driver: &SessionDriver{GUID: helperGUIDCarl2, Name: "Carl Two"}, OtherDriver: &SessionDriver{}},
		},
	}
}

func TestSessionResultsGetCrashes(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		all, env, car     int
	}{
		{"two crashes", helperGUIDAlice, helperModelFerrari, 2, 1, 1},
		{"only other car in a collision", helperGUIDBob, helperModelFerrari, 0, 0, 0},
		{"driver swap by normalised guid", helperGUIDSwap, helperModelPorsche, 1, 1, 0},
		{"driver swap by single guid", helperGUIDCarl2, helperModelPorsche, 1, 1, 0},
		{"no laps no crashes", helperGUIDDan, helperModelFerrari, 0, 0, 0},
		{"unknown driver", "9999", helperModelFerrari, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetCrashes(tc.guid, tc.model); got != tc.all {
				t.Errorf("GetCrashes = %d, want %d", got, tc.all)
			}

			if got := results.GetCrashesOfType(tc.guid, tc.model, "COLLISION_WITH_ENV"); got != tc.env {
				t.Errorf("GetCrashesOfType(env) = %d, want %d", got, tc.env)
			}

			if got := results.GetCrashesOfType(tc.guid, tc.model, "COLLISION_WITH_CAR"); got != tc.car {
				t.Errorf("GetCrashesOfType(car) = %d, want %d", got, tc.car)
			}
		})
	}
}

func TestSessionResultsGetAverageLapTime(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		want              time.Duration
	}{
		// first lap is ignored, average of 98.0s and 99.0s
		{"clean laps", helperGUIDAlice, helperModelFerrari, 98500 * time.Millisecond},
		// first lap ignored, cut lap ignored
		{"cut lap ignored", helperGUIDBob, helperModelFerrari, 101 * time.Second},
		// all laps of the car count, regardless of who drove: (99.0 + 104.0) / 2
		{"driver swap uses car laps", helperGUIDSwap, helperModelPorsche, 101500 * time.Millisecond},
		{"no laps", helperGUIDDan, helperModelFerrari, 0},
		{"wrong model", helperGUIDAlice, helperModelPorsche, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetAverageLapTime(tc.guid, tc.model); got != tc.want {
				t.Errorf("GetAverageLapTime = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSessionResultsGetOverallAverageLapTime(t *testing.T) {
	results := newHelperResults()

	// the first lap of every car and the cut lap are excluded:
	// (98000 + 99000 + 99000 + 101000 + 104000) / 5 = 100200ms
	if got, want := results.GetOverallAverageLapTime(), 100200*time.Millisecond; got != want {
		t.Errorf("GetOverallAverageLapTime = %s, want %s", got, want)
	}

	t.Run("first lap of each car is ignored", func(t *testing.T) {
		results := &SessionResults{Laps: []*SessionLap{
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 120000, 0, nil, "", uuid.Nil),
			helperLap(2, helperGUIDBob, "", helperModelFerrari, 130000, 0, nil, "", uuid.Nil),
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 100000, 0, nil, "", uuid.Nil),
			helperLap(2, helperGUIDBob, "", helperModelFerrari, 101000, 0, nil, "", uuid.Nil),
		}}

		if got, want := results.GetOverallAverageLapTime(), 100500*time.Millisecond; got != want {
			t.Errorf("GetOverallAverageLapTime = %s, want %s", got, want)
		}
	})

	t.Run("laps slower than 107% of the average are ignored", func(t *testing.T) {
		results := &SessionResults{Laps: []*SessionLap{
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 100000, 0, nil, "", uuid.Nil),
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 100000, 0, nil, "", uuid.Nil),
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 200000, 0, nil, "", uuid.Nil),
		}}

		if got, want := results.GetOverallAverageLapTime(), 100*time.Second; got != want {
			t.Errorf("GetOverallAverageLapTime = %s, want %s", got, want)
		}
	})

	t.Run("no laps to average", func(t *testing.T) {
		for _, results := range []*SessionResults{
			{},
			{Laps: []*SessionLap{helperLap(1, helperGUIDAlice, "", helperModelFerrari, 100000, 0, nil, "", uuid.Nil)}},
		} {
			if got := results.GetOverallAverageLapTime(); got != 0 {
				t.Errorf("GetOverallAverageLapTime = %s, want 0", got)
			}
		}
	})
}

func TestSessionResultsGetAverageLapTimeFiltering(t *testing.T) {
	newResults := func(lapTimes ...int) *SessionResults {
		results := &SessionResults{
			Cars: []*SessionCar{
				{CarID: 1, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDAlice, GuidsList: []string{helperGUIDAlice}}},
			},
		}

		for _, lapTime := range lapTimes {
			results.Laps = append(results.Laps, helperLap(1, helperGUIDAlice, "", helperModelFerrari, lapTime, 0, nil, "", uuid.Nil))
		}

		return results
	}

	for _, tc := range []struct {
		name     string
		lapTimes []int
		want     time.Duration
	}{
		// first lap ignored, 200s is slower than 107% of the 100s average
		{"slow lap ignored", []int{100000, 100000, 200000}, 100 * time.Second},
		{"slow lap after several laps ignored", []int{150000, 100000, 102000, 120000, 101000}, 101 * time.Second},
		{"lap within 107% counts", []int{150000, 100000, 106000}, 103 * time.Second},
		{"single lap", []int{100000}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := newResults(tc.lapTimes...).GetAverageLapTime(helperGUIDAlice, helperModelFerrari); got != tc.want {
				t.Errorf("GetAverageLapTime = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSessionResultsGetConsistency(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		want              float64
	}{
		// best 98.0s, average 98.5s: 100 - (0.5 / 98 * 100) = 99.489...
		{"consistent driver", helperGUIDAlice, helperModelFerrari, 99.49},
		// best clean lap 101.0s (95.0s is cut), average 101.0s
		{"cut lap ignored", helperGUIDBob, helperModelFerrari, 100},
		// best lap and average are both taken from the whole car: best 99.0s, average 101.5s
		{"swap by normalised guid", helperGUIDSwap, helperModelPorsche, 97.47},
		{"swap by single guid", helperGUIDCarl1, helperModelPorsche, 97.47},
		{"no laps", helperGUIDDan, helperModelFerrari, 0},
		{"unknown driver", "9999", helperModelFerrari, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetConsistency(tc.guid, tc.model); got != tc.want {
				t.Errorf("GetConsistency = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionResultsGetPosForLap(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		lap               int64
		want              int
	}{
		{"alice lap 1", helperGUIDAlice, helperModelFerrari, 1, 1},
		{"bob lap 1", helperGUIDBob, helperModelFerrari, 1, 2},
		{"swap lap 1", helperGUIDSwap, helperModelPorsche, 1, 3},
		{"swap lap 2 driven by second driver", helperGUIDSwap, helperModelPorsche, 2, 3},
		{"swap lap 2 asked with first driver guid", helperGUIDCarl1, helperModelPorsche, 2, 3},
		{"alice lap 3", helperGUIDAlice, helperModelFerrari, 3, 1},
		{"bob lap 3", helperGUIDBob, helperModelFerrari, 3, 2},
		{"lap not driven", helperGUIDAlice, helperModelFerrari, 4, 0},
		{"lap 0", helperGUIDAlice, helperModelFerrari, 0, 0},
		{"driver without laps", helperGUIDDan, helperModelFerrari, 1, 0},
		{"wrong model", helperGUIDAlice, helperModelPorsche, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetPosForLap(tc.guid, tc.model, tc.lap); got != tc.want {
				t.Errorf("GetPosForLap = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSessionResultsGetLastLapPos(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		want              int
	}{
		{"alice", helperGUIDAlice, helperModelFerrari, 1},
		{"bob", helperGUIDBob, helperModelFerrari, 2},
		{"swap by normalised guid", helperGUIDSwap, helperModelPorsche, 3},
		{"swap by second driver guid", helperGUIDCarl2, helperModelPorsche, 3},
		{"no laps", helperGUIDDan, helperModelFerrari, 0},
		{"unknown driver", "9999", helperModelFerrari, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetLastLapPos(tc.guid, tc.model); got != tc.want {
				t.Errorf("GetLastLapPos = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSessionResultsIsFastestLap(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name       string
		time, cuts int
		want       bool
	}{
		{"fastest clean lap", 98000, 0, true},
		{"slower lap", 99000, 0, false},
		{"faster than everything", 90000, 0, true},
		{"cut lap is never fastest", 95000, 2, false},
		{"cut lap time without cuts beats clean laps", 95000, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.IsFastestLap(tc.time, tc.cuts); got != tc.want {
				t.Errorf("IsFastestLap(%d, %d) = %t, want %t", tc.time, tc.cuts, got, tc.want)
			}
		})
	}

	if !(&SessionResults{}).IsFastestLap(100000, 0) {
		t.Error("IsFastestLap on empty results should be true")
	}
}

func TestSessionResultsIsDriversFastestLap(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		time, cuts        int
		want              bool
	}{
		{"alice fastest", helperGUIDAlice, helperModelFerrari, 98000, 0, true},
		{"alice slower", helperGUIDAlice, helperModelFerrari, 99000, 0, false},
		{"bob cut lap ignored", helperGUIDBob, helperModelFerrari, 101000, 0, true},
		{"bob slower", helperGUIDBob, helperModelFerrari, 102000, 0, false},
		{"cut lap", helperGUIDBob, helperModelFerrari, 95000, 2, false},
		{"other drivers laps don't count", helperGUIDBob, helperModelFerrari, 100000, 0, true},
		{"other model", helperGUIDAlice, helperModelPorsche, 200000, 0, true},
		{"swap driver only own laps", helperGUIDCarl1, helperModelPorsche, 104000, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.IsDriversFastestLap(tc.guid, tc.model, tc.time, tc.cuts); got != tc.want {
				t.Errorf("IsDriversFastestLap = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestSessionResultsGetDriversFastestLap(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		want              *SessionLap
	}{
		{"alice", helperGUIDAlice, helperModelFerrari, results.Laps[3]},
		{"bob ignores cut lap", helperGUIDBob, helperModelFerrari, results.Laps[7]},
		{"swap second driver", helperGUIDCarl2, helperModelPorsche, results.Laps[5]},
		{"no laps", helperGUIDDan, helperModelFerrari, nil},
		{"wrong model", helperGUIDAlice, helperModelPorsche, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetDriversFastestLap(tc.guid, tc.model); got != tc.want {
				t.Errorf("GetDriversFastestLap = %+v, want %+v", got, tc.want)
			}
		})
	}

	onlyCut := &SessionResults{Laps: []*SessionLap{
		helperLap(1, helperGUIDAlice, "Alice", helperModelFerrari, 90000, 1, nil, "", uuid.Nil),
	}}

	if got := onlyCut.GetDriversFastestLap(helperGUIDAlice, helperModelFerrari); got != nil {
		t.Errorf("GetDriversFastestLap with only cut laps = %+v, want nil", got)
	}
}

func TestSessionResultsIsFastestSector(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name               string
		sector, time, cuts int
		want               bool
	}{
		{"sector 1 fastest (cut lap 28.0 ignored)", 0, 29000, 0, true},
		{"sector 1 slower", 0, 30000, 0, false},
		{"sector 2 fastest", 1, 35000, 0, true},
		{"sector 2 slower", 1, 36000, 0, false},
		{"sector 3 fastest", 2, 30000, 0, true},
		{"sector 3 slower", 2, 30500, 0, false},
		{"cut", 0, 1000, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.IsFastestSector(tc.sector, tc.time, tc.cuts); got != tc.want {
				t.Errorf("IsFastestSector = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestSessionResultsIsDriversFastestSector(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model  string
		sector, time, cuts int
		want               bool
	}{
		{"bob sector 1 fastest clean", helperGUIDBob, helperModelFerrari, 0, 30500, 0, true},
		{"bob sector 1 slower", helperGUIDBob, helperModelFerrari, 0, 31000, 0, false},
		{"alice sector 2", helperGUIDAlice, helperModelFerrari, 1, 38500, 0, true},
		{"alice sector 2 slower", helperGUIDAlice, helperModelFerrari, 1, 39000, 0, false},
		{"other drivers ignored", helperGUIDDan, helperModelFerrari, 0, 50000, 0, true},
		{"cut", helperGUIDAlice, helperModelFerrari, 0, 1000, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.IsDriversFastestSector(tc.guid, tc.model, tc.sector, tc.time, tc.cuts); got != tc.want {
				t.Errorf("IsDriversFastestSector = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestSessionResultsGetNumSectors(t *testing.T) {
	results := newHelperResults()

	if got, want := results.GetNumSectors(), []int{1, 1, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("GetNumSectors = %v, want %v", got, want)
	}

	noSectors := &SessionResults{Laps: []*SessionLap{{LapTime: 1000}}}

	if got := noSectors.GetNumSectors(); len(got) != 0 {
		t.Errorf("GetNumSectors without sectors = %v, want empty", got)
	}

	if got := (&SessionResults{}).GetNumSectors(); len(got) != 0 {
		t.Errorf("GetNumSectors without laps = %v, want empty", got)
	}

	// the lap with the most sectors counts, not just the first lap
	moreSectorsLater := &SessionResults{Laps: []*SessionLap{
		{LapTime: 1000, Sectors: []int{500, 500}},
		{LapTime: 1000, Sectors: []int{300, 300, 400}},
	}}

	if got, want := moreSectorsLater.GetNumSectors(), []int{1, 1, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("GetNumSectors with differing sector counts = %v, want %v", got, want)
	}
}

func TestSessionResultsGetPotentialLapDifferingSectors(t *testing.T) {
	if got := (&SessionResults{}).GetPotentialLap(helperGUIDAlice, helperModelFerrari); got != 0 {
		t.Errorf("GetPotentialLap without laps = %s, want 0", got)
	}

	results := &SessionResults{
		Cars: []*SessionCar{
			{CarID: 1, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDAlice, GuidsList: []string{helperGUIDAlice}}},
		},
		Laps: []*SessionLap{
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 100000, 0, []int{50000, 50000}, "", uuid.Nil),
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 99000, 0, []int{30000, 40000, 29000}, "", uuid.Nil),
		},
	}

	// sectors are compared by index: 30.0 + 40.0 + 29.0
	if got, want := results.GetPotentialLap(helperGUIDAlice, helperModelFerrari), 99*time.Second; got != want {
		t.Errorf("GetPotentialLap = %s, want %s", got, want)
	}
}

func TestSessionResultsGetDrivers(t *testing.T) {
	oldShortened := UseShortenedDriverNames
	defer func() { UseShortenedDriverNames = oldShortened }()

	results := newHelperResults()
	results.Result = append(results.Result, &SessionResult{DriverGUID: "1010", CarModel: helperModelFerrari, DriverName: ""})

	UseShortenedDriverNames = false

	if got, want := results.GetDrivers(), "Alice Smith, Bob Jones, Carl One, Dan Nolaps"; got != want {
		t.Errorf("GetDrivers = %q, want %q", got, want)
	}

	UseShortenedDriverNames = true

	if got, want := results.GetDrivers(), "Alice S., Bob J., Carl O., Dan N."; got != want {
		t.Errorf("GetDrivers (shortened) = %q, want %q", got, want)
	}

	if got := (&SessionResults{}).GetDrivers(); got != "" {
		t.Errorf("GetDrivers on empty results = %q, want empty", got)
	}
}

func TestSessionResultsGetTime(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		time              int
		penalty           bool
		want              time.Duration
	}{
		{"plain", helperGUIDAlice, helperModelFerrari, 98000, false, 98 * time.Second},
		{"penalty flag without penalty", helperGUIDAlice, helperModelFerrari, 98000, true, 98 * time.Second},
		{"time penalty not applied", helperGUIDBob, helperModelFerrari, 298000, false, 298 * time.Second},
		{"time penalty applied", helperGUIDBob, helperModelFerrari, 298000, true, 303 * time.Second},
		{"driver swap", helperGUIDSwap, helperModelPorsche, 1500, false, 1500 * time.Millisecond},
		{"driver without laps", helperGUIDDan, helperModelFerrari, 98000, false, 0},
		{"unknown driver", "9999", helperModelFerrari, 98000, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetTime(tc.time, tc.guid, tc.model, tc.penalty); got != tc.want {
				t.Errorf("GetTime = %s, want %s", got, tc.want)
			}
		})
	}

	t.Run("lap penalty in race subtracts last lap", func(t *testing.T) {
		results := newHelperResults()
		results.Result[0].HasPenalty = true
		results.Result[0].PenaltyTime = 10 * time.Second
		results.Result[0].LapPenalty = 1

		// 297s + 10s - 1 * last lap (99s)
		if got, want := results.GetTime(297000, helperGUIDAlice, helperModelFerrari, true), 208*time.Second; got != want {
			t.Errorf("GetTime = %s, want %s", got, want)
		}

		results.Type = serverconfig.SessionTypeQualifying

		if got, want := results.GetTime(297000, helperGUIDAlice, helperModelFerrari, true), 307*time.Second; got != want {
			t.Errorf("GetTime (qualifying) = %s, want %s", got, want)
		}
	})
}

func TestSessionResultsGetNumLapsAndCuts(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		laps, cuts        int
	}{
		{"alice", helperGUIDAlice, helperModelFerrari, 3, 0},
		{"bob", helperGUIDBob, helperModelFerrari, 3, 2},
		{"swap counts the whole car", helperGUIDSwap, helperModelPorsche, 3, 0},
		{"no laps", helperGUIDDan, helperModelFerrari, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetNumLaps(tc.guid, tc.model); got != tc.laps {
				t.Errorf("GetNumLaps = %d, want %d", got, tc.laps)
			}

			if got := results.GetCuts(tc.guid, tc.model); got != tc.cuts {
				t.Errorf("GetCuts = %d, want %d", got, tc.cuts)
			}
		})
	}

	results.Result[0].LapPenalty = 2

	if got := results.GetNumLaps(helperGUIDAlice, helperModelFerrari); got != 1 {
		t.Errorf("GetNumLaps with lap penalty = %d, want 1", got)
	}
}

func TestSessionResultsGetLastLapTime(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		want              time.Duration
	}{
		{"alice", helperGUIDAlice, helperModelFerrari, 99 * time.Second},
		{"bob", helperGUIDBob, helperModelFerrari, 101 * time.Second},
		{"swap last lap of the car", helperGUIDSwap, helperModelPorsche, 104 * time.Second},
		{"swap by second driver guid", helperGUIDCarl2, helperModelPorsche, 104 * time.Second},
		// sentinel value for "no laps"
		{"no laps", helperGUIDDan, helperModelFerrari, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetLastLapTime(tc.guid, tc.model); got != tc.want {
				t.Errorf("GetLastLapTime = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSessionResultsGetPotentialLap(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		want              time.Duration
	}{
		// 29.0 + 38.5 + 30.0
		{"best sectors of different laps", helperGUIDAlice, helperModelFerrari, 97500 * time.Millisecond},
		// cut lap (28.0, 38.0, 29.0) ignored: 30.5 + 39.5 + 31.0
		{"cut lap ignored", helperGUIDBob, helperModelFerrari, 101 * time.Second},
		// all laps of the car count, like for the other per-car helpers: 33.0 + 35.0 + 30.0
		{"swap by normalised guid", helperGUIDSwap, helperModelPorsche, 98 * time.Second},
		{"swap by single guid", helperGUIDCarl1, helperModelPorsche, 98 * time.Second},
		{"unknown driver", "9999", helperModelFerrari, 0},
		{"no laps", helperGUIDDan, helperModelFerrari, 0},
		{"wrong model", helperGUIDAlice, helperModelPorsche, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetPotentialLap(tc.guid, tc.model); got != tc.want {
				t.Errorf("GetPotentialLap = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSessionResultsGetDriverPosition(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name, guid, model string
		want              int
	}{
		{"first", helperGUIDAlice, helperModelFerrari, 1},
		{"second", helperGUIDBob, helperModelFerrari, 2},
		{"swap", helperGUIDSwap, helperModelPorsche, 3},
		{"no laps", helperGUIDDan, helperModelFerrari, 4},
		{"wrong model", helperGUIDAlice, helperModelPorsche, 0},
		{"unknown", "9999", helperModelFerrari, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.GetDriverPosition(tc.guid, tc.model); got != tc.want {
				t.Errorf("GetDriverPosition = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSessionResultsFastestLap(t *testing.T) {
	results := newHelperResults()

	if got := results.FastestLap(); got != results.Laps[3] {
		t.Errorf("FastestLap = %+v, want lap with 98000ms (cut lap with 95000ms must be ignored)", got)
	}

	if got := (&SessionResults{}).FastestLap(); got != nil {
		t.Errorf("FastestLap on empty results = %+v, want nil", got)
	}

	// FastestLap must not reorder the laps of the result
	if results.Laps[0].LapTime != 100000 || results.Laps[8].LapTime != 104000 {
		t.Error("FastestLap modified the order of results.Laps")
	}
}

func TestSessionResultsFastestLapInClass(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name    string
		classID uuid.UUID
		want    *SessionLap
	}{
		{"ferrari class ignores cut lap", helperClassFerrari, results.Laps[3]},
		{"porsche class", helperClassPorsche, results.Laps[5]},
		{"unknown class", uuid.MustParse("33333333-3333-3333-3333-333333333333"), nil},
		{"nil class", uuid.Nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := results.FastestLapInClass(tc.classID); got != tc.want {
				t.Errorf("FastestLapInClass = %+v, want %+v", got, tc.want)
			}
		})
	}

	if got := (&SessionResults{}).FastestLapInClass(helperClassFerrari); got != nil {
		t.Errorf("FastestLapInClass on empty results = %+v, want nil", got)
	}
}

func TestSessionResultsFastestLapOnlyCutLaps(t *testing.T) {
	results := &SessionResults{Laps: []*SessionLap{
		helperLap(1, helperGUIDAlice, "", helperModelFerrari, 90000, 1, nil, "", helperClassFerrari),
		helperLap(2, helperGUIDBob, "", helperModelFerrari, 95000, 2, nil, "", helperClassFerrari),
		helperLap(3, helperGUIDCarl1, "", helperModelPorsche, 99000, 0, nil, "", helperClassPorsche),
	}}

	if got := results.FastestLapInClass(helperClassFerrari); got != nil {
		t.Errorf("FastestLapInClass with only cut laps = %+v, want nil", got)
	}

	if got := results.FastestLapInClass(helperClassPorsche); got != results.Laps[2] {
		t.Errorf("FastestLapInClass = %+v, want the clean porsche lap", got)
	}

	if got := results.FastestLap(); got != results.Laps[2] {
		t.Errorf("FastestLap = %+v, want the only clean lap", got)
	}

	results.Laps = results.Laps[:2]

	if got := results.FastestLap(); got != nil {
		t.Errorf("FastestLap with only cut laps = %+v, want nil", got)
	}
}

func TestSessionResultsFastestSectorMissingSectors(t *testing.T) {
	results := &SessionResults{Laps: []*SessionLap{
		helperLap(1, helperGUIDAlice, "", helperModelFerrari, 100000, 0, []int{30000, 40000, 30000}, "", uuid.Nil),
		helperLap(1, helperGUIDAlice, "", helperModelFerrari, 70000, 0, []int{30000, 40000}, "", uuid.Nil),
		helperLap(1, helperGUIDAlice, "", helperModelFerrari, 1000, 0, nil, "", uuid.Nil),
	}}

	// laps without that sector are skipped instead of panicking
	if !results.IsFastestSector(2, 30000, 0) {
		t.Error("IsFastestSector(2) = false, want true")
	}

	if results.IsFastestSector(1, 41000, 0) {
		t.Error("IsFastestSector(1, slower) = true, want false")
	}

	if !results.IsDriversFastestSector(helperGUIDAlice, helperModelFerrari, 2, 30000, 0) {
		t.Error("IsDriversFastestSector(2) = false, want true")
	}

	if results.IsDriversFastestSector(helperGUIDAlice, helperModelFerrari, 1, 41000, 0) {
		t.Error("IsDriversFastestSector(1, slower) = true, want false")
	}
}

func TestSessionResultsDriverSwaps(t *testing.T) {
	results := newHelperResults()

	t.Run("ResultHasMultipleDrivers", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			result *SessionResult
			want   bool
		}{
			{"single driver", results.Result[0], false},
			{"swap car", results.Result[2], true},
			{"no laps", results.Result[3], false},
			{"unknown car", &SessionResult{DriverGUID: "9999", CarModel: helperModelFerrari}, false},
		} {
			if got := results.ResultHasMultipleDrivers(tc.result); got != tc.want {
				t.Errorf("%s: ResultHasMultipleDrivers = %t, want %t", tc.name, got, tc.want)
			}
		}
	})

	t.Run("SessionCar.HasMultipleDrivers", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			car  *SessionCar
			want bool
		}{
			{"one guid", &SessionCar{Driver: SessionDriver{GUID: "1", GuidsList: []string{"1"}}}, false},
			{"guids list", &SessionCar{Driver: SessionDriver{GUID: "1", GuidsList: []string{"1", "2"}}}, true},
			{"joined guid", &SessionCar{Driver: SessionDriver{GUID: "1;2"}}, true},
			{"empty", &SessionCar{}, false},
		} {
			if got := tc.car.HasMultipleDrivers(); got != tc.want {
				t.Errorf("%s: HasMultipleDrivers = %t, want %t", tc.name, got, tc.want)
			}
		}
	})

	t.Run("NumberOfDriverSwaps", func(t *testing.T) {
		for _, tc := range []struct {
			carID, want int
		}{
			{1, 0},
			{3, 2}, // Carl1 -> Carl2 -> Carl1
			{4, 0},
			{99, 0},
		} {
			if got := results.NumberOfDriverSwaps(tc.carID); got != tc.want {
				t.Errorf("NumberOfDriverSwaps(%d) = %d, want %d", tc.carID, got, tc.want)
			}
		}
	})

	t.Run("LapAssociatedWithGUIDAndModel", func(t *testing.T) {
		aliceLap, carl2Lap := results.Laps[0], results.Laps[5]

		for _, tc := range []struct {
			name, guid, model string
			lap               *SessionLap
			want              bool
		}{
			{"own lap", helperGUIDAlice, helperModelFerrari, aliceLap, true},
			{"own lap wrong model", helperGUIDAlice, helperModelPorsche, aliceLap, false},
			{"other drivers lap", helperGUIDBob, helperModelFerrari, aliceLap, false},
			{"swap partner via normalised guid", helperGUIDSwap, helperModelPorsche, carl2Lap, true},
			{"swap partner via single guid", helperGUIDCarl1, helperModelPorsche, carl2Lap, true},
			{"swap partner wrong model", helperGUIDCarl1, helperModelFerrari, carl2Lap, false},
			{"swap driver and unrelated lap", helperGUIDCarl1, helperModelPorsche, aliceLap, false},
		} {
			if got := results.LapAssociatedWithGUIDAndModel(tc.lap, tc.guid, tc.model); got != tc.want {
				t.Errorf("%s: LapAssociatedWithGUIDAndModel = %t, want %t", tc.name, got, tc.want)
			}
		}
	})
}

func TestSessionResultBestLapTyre(t *testing.T) {
	results := newHelperResults()

	for _, tc := range []struct {
		name   string
		result *SessionResult
		want   string
	}{
		{"alice", results.Result[0], "S"},
		{"bob", results.Result[1], "M"},
		{"swap", results.Result[2], "H"},
		{"no laps", results.Result[3], "?"},
		{"best lap not in laps", &SessionResult{CarID: 1, BestLap: 12345}, "?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.result.BestLapTyre(results); got != tc.want {
				t.Errorf("BestLapTyre = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSessionLapHelpers(t *testing.T) {
	lap := &SessionLap{LapTime: 95123, Cuts: 2, Sectors: []int{30000, 35123, 30000}}

	if got, want := lap.GetLapTime(), 95123*time.Millisecond; got != want {
		t.Errorf("GetLapTime = %s, want %s", got, want)
	}

	if got, want := lap.GetSector(1), 35123*time.Millisecond; got != want {
		t.Errorf("GetSector(1) = %s, want %s", got, want)
	}

	for _, tc := range []struct {
		name    string
		lapTime int
		cuts    int
		average time.Duration
		want    bool
	}{
		{"cut and faster than average", 95000, 2, 100 * time.Second, true},
		{"cut but slower than average", 95000, 2, 90 * time.Second, false},
		{"cut and equal to average", 95000, 1, 95 * time.Second, false},
		{"clean and faster than average", 95000, 0, 100 * time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &SessionLap{LapTime: tc.lapTime, Cuts: tc.cuts}

			if got := l.DidCheat(tc.average); got != tc.want {
				t.Errorf("DidCheat = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestSessionResultsHasHandicaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result []*SessionResult
		want   bool
	}{
		{"empty", nil, false},
		{"no handicaps", []*SessionResult{{}, {}}, false},
		{"ballast", []*SessionResult{{}, {BallastKG: 10}}, true},
		{"restrictor", []*SessionResult{{Restrictor: 5}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (&SessionResults{Result: tc.result}).HasHandicaps(); got != tc.want {
				t.Errorf("HasHandicaps = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestSessionResultsDriversHaveTeams(t *testing.T) {
	results := newHelperResults()

	if !results.DriversHaveTeams() {
		t.Error("DriversHaveTeams = false, want true (Alice is in team Red)")
	}

	// a team only counts for drivers that actually have a total time
	results.Result[0].TotalTime = 0

	if results.DriversHaveTeams() {
		t.Error("DriversHaveTeams = true, want false (only driver with a team has no time)")
	}

	results = newHelperResults()

	for _, car := range results.Cars {
		car.Driver.Team = ""
	}

	if results.DriversHaveTeams() {
		t.Error("DriversHaveTeams = true, want false (no teams)")
	}

	if (&SessionResults{}).DriversHaveTeams() {
		t.Error("DriversHaveTeams on empty results = true, want false")
	}
}

func TestAnonymiseDriverGUID(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"", "d41d8cd98f00b204e9800998ecf8427e"},
		{"abc", "900150983cd24fb0d6963f7d28e17f72"},
	} {
		if got := AnonymiseDriverGUID(tc.in); got != tc.want {
			t.Errorf("AnonymiseDriverGUID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if AnonymiseDriverGUID(helperGUIDAlice) == AnonymiseDriverGUID(helperGUIDBob) {
		t.Error("different GUIDs must not anonymise to the same value")
	}
}

func TestSessionResultsAnonymize(t *testing.T) {
	results := newHelperResults()
	results.Anonymize()

	alice, bob, carl1, carl2 := AnonymiseDriverGUID(helperGUIDAlice), AnonymiseDriverGUID(helperGUIDBob),
		AnonymiseDriverGUID(helperGUIDCarl1), AnonymiseDriverGUID(helperGUIDCarl2)

	if car := results.Cars[0]; car.Driver.GUID != alice || car.Driver.Name != "Alice S." || !reflect.DeepEqual(car.Driver.GuidsList, []string{alice}) {
		t.Errorf("car not anonymised: %+v", car.Driver)
	}

	if car := results.Cars[2]; car.Driver.GUID != carl1 || !reflect.DeepEqual(car.Driver.GuidsList, []string{carl1, carl2}) {
		t.Errorf("swap car not anonymised: %+v", car.Driver)
	}

	if lap := results.Laps[0]; lap.DriverGUID != alice || lap.DriverName != "Alice S." {
		t.Errorf("lap not anonymised: %s %s", lap.DriverGUID, lap.DriverName)
	}

	if result := results.Result[1]; result.DriverGUID != bob || result.DriverName != "Bob J." {
		t.Errorf("result not anonymised: %s %s", result.DriverGUID, result.DriverName)
	}

	if event := results.Events[0]; event.Driver.GUID != alice || event.Driver.Name != "Alice S." ||
		event.OtherDriver.GUID != bob || event.OtherDriver.Name != "Bob J." {
		t.Errorf("event not anonymised: %+v / %+v", event.Driver, event.OtherDriver)
	}

	if event := results.Events[1]; event.OtherDriver.GUID != AnonymiseDriverGUID("") {
		t.Errorf("empty other driver GUID should be hashed too, got %q", event.OtherDriver.GUID)
	}
}

func TestSessionResultsAnonymizeEventGuidsLists(t *testing.T) {
	results := newHelperResults()
	results.Events = []*SessionEvent{
		{
			CarID: 1, OtherCarID: 3, Type: "COLLISION_WITH_CAR",
			Driver:      &SessionDriver{GUID: helperGUIDAlice, GuidsList: []string{helperGUIDAlice}},
			OtherDriver: &SessionDriver{GUID: helperGUIDCarl1, GuidsList: []string{helperGUIDCarl1, helperGUIDCarl2}},
		},
	}

	// the other driver's list is longer than the driver's list, this used to panic
	results.Anonymize()

	event := results.Events[0]

	if want := []string{AnonymiseDriverGUID(helperGUIDAlice)}; !reflect.DeepEqual(event.Driver.GuidsList, want) {
		t.Errorf("Driver.GuidsList = %v, want %v", event.Driver.GuidsList, want)
	}

	if want := []string{AnonymiseDriverGUID(helperGUIDCarl1), AnonymiseDriverGUID(helperGUIDCarl2)}; !reflect.DeepEqual(event.OtherDriver.GuidsList, want) {
		t.Errorf("OtherDriver.GuidsList = %v, want %v", event.OtherDriver.GuidsList, want)
	}
}

func TestSessionResultsMaskDriverNames(t *testing.T) {
	oldShortened := UseShortenedDriverNames
	defer func() { UseShortenedDriverNames = oldShortened }()

	UseShortenedDriverNames = false

	results := newHelperResults()
	results.MaskDriverNames()

	if got := results.Cars[0].Driver.Name; got != "Alice Smith" {
		t.Errorf("names must stay unchanged without shortened names, got %q", got)
	}

	UseShortenedDriverNames = true

	results = newHelperResults()
	results.MaskDriverNames()

	for _, got := range []string{
		results.Cars[0].Driver.Name,
		results.Laps[0].DriverName,
		results.Result[0].DriverName,
		results.Events[0].Driver.Name,
		results.Events[0].OtherDriver.Name,
	} {
		if got != "Alice S." && got != "Bob J." {
			t.Errorf("name not masked: %q", got)
		}
	}

	if results.Events[0].OtherDriver.Name != "Bob J." {
		t.Errorf("other driver = %q, want %q", results.Events[0].OtherDriver.Name, "Bob J.")
	}

	// GUIDs stay untouched
	if results.Cars[0].Driver.GUID != helperGUIDAlice {
		t.Errorf("MaskDriverNames changed GUID to %q", results.Cars[0].Driver.GUID)
	}
}

func TestSessionResultsRenameDriver(t *testing.T) {
	results := newHelperResults()
	results.RenameDriver(helperGUIDBob, "Robert")

	if got := results.Cars[1].Driver.Name; got != "Robert" {
		t.Errorf("car name = %q", got)
	}

	for _, lap := range results.Laps {
		if lap.DriverGUID == helperGUIDBob && lap.DriverName != "Robert" {
			t.Errorf("lap name = %q", lap.DriverName)
		}

		if lap.DriverGUID == helperGUIDAlice && lap.DriverName != "Alice Smith" {
			t.Errorf("other drivers lap renamed to %q", lap.DriverName)
		}
	}

	if got := results.Result[1].DriverName; got != "Robert" {
		t.Errorf("result name = %q", got)
	}

	if got := results.Events[0].OtherDriver.Name; got != "Robert" {
		t.Errorf("event other driver name = %q", got)
	}

	if got := results.Events[0].Driver.Name; got != "Alice Smith" {
		t.Errorf("event driver renamed to %q", got)
	}

	if got := results.Cars[0].Driver.Name; got != "Alice Smith" {
		t.Errorf("other car renamed to %q", got)
	}

	// unknown GUID: nothing changes
	results.RenameDriver("9999", "Nobody")

	for _, r := range results.Result {
		if r.DriverName == "Nobody" {
			t.Error("unknown GUID renamed a driver")
		}
	}
}

func resultOrder(results *SessionResults) []string {
	var order []string

	for _, r := range results.Result {
		order = append(order, r.DriverGUID)
	}

	return order
}

func TestSessionResultsFallBackSortRace(t *testing.T) {
	// Results only contain Bob, Alice is missing, Dan has no laps, "1006" only appears in the laps.
	results := &SessionResults{
		Type: serverconfig.SessionTypeRace,
		Cars: []*SessionCar{
			{CarID: 1, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDAlice, Name: "Alice Smith"}, BallastKG: 20},
			{CarID: 2, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDBob, Name: "Bob Jones"}},
			{CarID: 4, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDDan, Name: "Dan Nolaps"}},
		},
		Laps: []*SessionLap{
			helperLap(1, helperGUIDAlice, "Alice Smith", helperModelFerrari, 100000, 0, nil, "", uuid.Nil),
			helperLap(2, helperGUIDBob, "Bob Jones", helperModelFerrari, 101000, 0, nil, "", uuid.Nil),
			helperLap(5, "1006", "Eve Orphan", helperModelPorsche, 120000, 0, nil, "", helperClassPorsche),
			helperLap(2, helperGUIDBob, "Bob Jones", helperModelFerrari, 97000, 0, nil, "", uuid.Nil),
			helperLap(1, helperGUIDAlice, "Alice Smith", helperModelFerrari, 99000, 3, nil, "", uuid.Nil),
		},
		Result: []*SessionResult{
			{CarID: 2, CarModel: helperModelFerrari, DriverGUID: helperGUIDBob, DriverName: "Bob Jones", TotalTime: 1},
		},
	}

	results.FallBackSort()

	// Alice and Bob both have 2 laps, Bob crossed the line first on lap 2.
	// Eve has 1 lap, Dan none.
	if got, want := resultOrder(results), []string{helperGUIDBob, helperGUIDAlice, "1006", helperGUIDDan}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}

	byGUID := make(map[string]*SessionResult)

	for _, r := range results.Result {
		byGUID[r.DriverGUID] = r
	}

	for _, tc := range []struct {
		guid      string
		totalTime int
		bestLap   int
	}{
		{helperGUIDBob, 198000, 97000},
		{helperGUIDAlice, 199000, 100000}, // 99000 is cut
		{"1006", 120000, 120000},
		{helperGUIDDan, 0, 0},
	} {
		r := byGUID[tc.guid]

		if r.TotalTime != tc.totalTime {
			t.Errorf("%s: TotalTime = %d, want %d", tc.guid, r.TotalTime, tc.totalTime)
		}

		if r.BestLap != tc.bestLap {
			t.Errorf("%s: BestLap = %d, want %d", tc.guid, r.BestLap, tc.bestLap)
		}
	}

	if byGUID[helperGUIDAlice].BallastKG != 20 || byGUID[helperGUIDAlice].CarID != 1 {
		t.Errorf("result created from car lost car data: %+v", byGUID[helperGUIDAlice])
	}

	if r := byGUID["1006"]; r.CarID != 5 || r.CarModel != helperModelPorsche || r.DriverName != "Eve Orphan" || r.ClassID != helperClassPorsche {
		t.Errorf("result created from lap lost lap data: %+v", r)
	}

	// the orphan driver must have been added to the car list too
	if len(results.Cars) != 4 {
		t.Fatalf("len(Cars) = %d, want 4", len(results.Cars))
	}

	if car := results.Cars[3]; car.Driver.GUID != "1006" || car.Model != helperModelPorsche || car.CarID != 5 || !reflect.DeepEqual(car.Driver.GuidsList, []string{"1006"}) {
		t.Errorf("added car = %+v", car)
	}
}

func TestSessionResultsFallBackSortTimePenalty(t *testing.T) {
	results := &SessionResults{
		Type: serverconfig.SessionTypeRace,
		Cars: []*SessionCar{
			{CarID: 1, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDAlice, GuidsList: []string{helperGUIDAlice}}},
			{CarID: 2, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDBob, GuidsList: []string{helperGUIDBob}}},
		},
		Laps: []*SessionLap{
			// Bob finishes 3s ahead on the road, but has a 5s time penalty
			helperLap(2, helperGUIDBob, "", helperModelFerrari, 100000, 0, nil, "", uuid.Nil),
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 103000, 0, nil, "", uuid.Nil),
		},
		Result: []*SessionResult{
			{CarID: 2, CarModel: helperModelFerrari, DriverGUID: helperGUIDBob, HasPenalty: true, PenaltyTime: 5 * time.Second},
			{CarID: 1, CarModel: helperModelFerrari, DriverGUID: helperGUIDAlice},
		},
	}

	results.FallBackSort()

	if got, want := resultOrder(results), []string{helperGUIDAlice, helperGUIDBob}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}

	bob := results.Result[1]

	// TotalTime is the plain race time in ms, the penalty is added by GetTime (and must only be added once)
	if bob.TotalTime != 100000 {
		t.Errorf("Bob TotalTime = %d, want 100000", bob.TotalTime)
	}

	if got, want := results.GetTime(bob.TotalTime, bob.DriverGUID, bob.CarModel, true), 105*time.Second; got != want {
		t.Errorf("Bob GetTime with penalty = %s, want %s", got, want)
	}
}

func TestSessionResultsFallBackSortDisqualified(t *testing.T) {
	results := &SessionResults{
		Type: serverconfig.SessionTypeRace,
		Cars: []*SessionCar{
			{CarID: 1, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDAlice}},
			{CarID: 2, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDBob}},
		},
		Laps: []*SessionLap{
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 100000, 0, nil, "", uuid.Nil),
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 100000, 0, nil, "", uuid.Nil),
			helperLap(2, helperGUIDBob, "", helperModelFerrari, 110000, 0, nil, "", uuid.Nil),
		},
		Result: []*SessionResult{
			{CarID: 1, CarModel: helperModelFerrari, DriverGUID: helperGUIDAlice, Disqualified: true},
			{CarID: 2, CarModel: helperModelFerrari, DriverGUID: helperGUIDBob},
		},
	}

	results.FallBackSort()

	if got, want := resultOrder(results), []string{helperGUIDBob, helperGUIDAlice}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v (disqualified drivers go to the back)", got, want)
	}
}

func TestSessionResultsFallBackSortQualifying(t *testing.T) {
	results := &SessionResults{
		Type: serverconfig.SessionTypeQualifying,
		Cars: []*SessionCar{
			{CarID: 1, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDAlice}},
			{CarID: 2, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDBob}},
			{CarID: 4, Model: helperModelFerrari, Driver: SessionDriver{GUID: helperGUIDDan}},
		},
		Laps: []*SessionLap{
			// Alice drives more laps, but Bob has the faster best lap
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 100000, 0, nil, "", uuid.Nil),
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 99000, 0, nil, "", uuid.Nil),
			helperLap(1, helperGUIDAlice, "", helperModelFerrari, 90000, 1, nil, "", uuid.Nil),
			helperLap(2, helperGUIDBob, "", helperModelFerrari, 98000, 0, nil, "", uuid.Nil),
		},
		Result: []*SessionResult{
			{CarID: 4, CarModel: helperModelFerrari, DriverGUID: helperGUIDDan},
			{CarID: 1, CarModel: helperModelFerrari, DriverGUID: helperGUIDAlice},
			{CarID: 2, CarModel: helperModelFerrari, DriverGUID: helperGUIDBob},
		},
	}

	results.FallBackSort()

	if got, want := resultOrder(results), []string{helperGUIDBob, helperGUIDAlice, helperGUIDDan}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}

	if got := results.Result[1].BestLap; got != 99000 {
		t.Errorf("Alice BestLap = %d, want 99000 (cut lap ignored)", got)
	}
}

// newHelperResultsWithCarZero adds a car with the (valid) ID 0 that has laps, cuts, a crash and a lap penalty,
// so lookups of unknown drivers can be told apart from car 0.
func newHelperResultsWithCarZero() *SessionResults {
	results := newHelperResults()

	results.Cars = append(results.Cars, &SessionCar{CarID: 0, Model: helperModelFerrari, Driver: SessionDriver{GUID: "1000", GuidsList: []string{"1000"}, Name: "Zed Zero"}})
	results.Laps = append(results.Laps,
		helperLap(0, "1000", "Zed Zero", helperModelFerrari, 110000, 1, []int{36000, 38000, 36000}, "M", helperClassFerrari),
		helperLap(0, "1000", "Zed Zero", helperModelFerrari, 108000, 0, []int{35000, 38000, 35000}, "M", helperClassFerrari),
		helperLap(0, "1000", "Zed Zero", helperModelFerrari, 107000, 0, []int{35000, 37000, 35000}, "M", helperClassFerrari),
	)
	results.Result = append(results.Result, &SessionResult{CarID: 0, CarModel: helperModelFerrari, DriverGUID: "1000", DriverName: "Zed Zero", HasPenalty: true, PenaltyTime: 10 * time.Second, LapPenalty: 1})
	results.Events = append(results.Events, &SessionEvent{CarID: 0, Type: "COLLISION_WITH_ENV", Driver: &SessionDriver{GUID: "1000"}, OtherDriver: &SessionDriver{}})

	return results
}

func TestSessionResultsFindCarIDForGUIDAndModel(t *testing.T) {
	results := newHelperResultsWithCarZero()

	for _, tc := range []struct {
		name, guid, model string
		wantID            int
		wantOK            bool
	}{
		{"car zero", "1000", helperModelFerrari, 0, true},
		{"single driver", helperGUIDBob, helperModelFerrari, 2, true},
		{"swap by normalised guid", helperGUIDSwap, helperModelPorsche, 3, true},
		{"swap by second driver", helperGUIDCarl2, helperModelPorsche, 3, true},
		{"unknown guid", "9999", helperModelFerrari, -1, false},
		{"wrong model", helperGUIDBob, helperModelPorsche, -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := results.FindCarIDForGUIDAndModel(tc.guid, tc.model)

			if ok != tc.wantOK || (ok && id != tc.wantID) {
				t.Errorf("FindCarIDForGUIDAndModel = (%d, %t), want (%d, %t)", id, ok, tc.wantID, tc.wantOK)
			}
		})
	}
}

func TestSessionResultsUnknownDriverDoesNotMatchCarZero(t *testing.T) {
	results := newHelperResultsWithCarZero()

	// sanity check: car 0 itself has data
	if got := results.GetNumLaps("1000", helperModelFerrari); got != 2 {
		t.Fatalf("GetNumLaps(car 0) = %d, want 2 (3 laps - 1 lap penalty)", got)
	}

	for _, model := range []string{helperModelFerrari, helperModelPorsche} {
		guid := "9999"

		if got := results.GetCrashes(guid, model); got != 0 {
			t.Errorf("%s: GetCrashes = %d, want 0", model, got)
		}

		if got := results.GetCrashesOfType(guid, model, "COLLISION_WITH_ENV"); got != 0 {
			t.Errorf("%s: GetCrashesOfType = %d, want 0", model, got)
		}

		if got := results.GetNumLaps(guid, model); got != 0 {
			t.Errorf("%s: GetNumLaps = %d, want 0", model, got)
		}

		if got := results.GetCuts(guid, model); got != 0 {
			t.Errorf("%s: GetCuts = %d, want 0", model, got)
		}

		if got := results.GetTime(100000, guid, model, true); got != 0 {
			t.Errorf("%s: GetTime = %s, want 0", model, got)
		}

		if got := results.GetAverageLapTime(guid, model); got != 0 {
			t.Errorf("%s: GetAverageLapTime = %s, want 0", model, got)
		}

		if got := results.GetLastLapTime(guid, model); got != 1 {
			t.Errorf("%s: GetLastLapTime = %s, want the 'no laps' sentinel 1ns", model, got)
		}

		if car, err := results.FindCarByGUIDAndModel(guid, model); err != ErrSessionCarNotFound {
			t.Errorf("%s: FindCarByGUIDAndModel = %+v, %v, want ErrSessionCarNotFound", model, car, err)
		}
	}
}
