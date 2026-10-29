package results

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
)

// PageSize is the number of results per page of Loader.List.
const PageSize = 25

// TimeAttackSuffix is appended to the file name of combined time attack results.
const TimeAttackSuffix = "-time-attack"

var ErrPageNotFound = errors.New("servermanager: results page not found")

// Filter narrows down the results list. Empty fields do not filter.
type Filter struct {
	Type   string // session type as in the file name, e.g. "race". "booking" matches BOOK files.
	Track  string // case-insensitive substring of the track name, readable track name or layout
	Driver string // case-insensitive substring of a driver name, or an exact driver GUID
}

func (f Filter) IsActive() bool {
	return f.Type != "" || f.Track != "" || f.Driver != ""
}

// needsLoading is true if the filter can only be evaluated by looking into the results files.
func (f Filter) needsLoading() bool {
	return f.Track != "" || f.Driver != ""
}

// matchesFileName applies the session type filter, which can be decided from the file name alone.
func (f Filter) matchesFileName(name string) bool {
	if f.Type == "" {
		return true
	}

	name = strings.TrimSuffix(name, ".json")
	name = strings.TrimSuffix(name, TimeAttackSuffix)

	sessionType := strings.ToUpper(name[strings.LastIndex(name, "_")+1:])

	want := strings.ToUpper(f.Type)

	if want == "BOOKING" {
		want = string(serverconfig.SessionTypeBooking)
	}

	// RACEx2 counts as a race
	return sessionType == want || (want == string(serverconfig.SessionTypeRace) && strings.HasPrefix(sessionType, want))
}

// resultSummary is the part of a results file that the track and driver filters look at.
type resultSummary struct {
	size    int64
	modTime time.Time

	track   string // lowercased track names and layout, as shown in the list and as stored
	drivers []string
	guids   map[string]bool
}

func (s *resultSummary) matches(f Filter) bool {
	if f.Track != "" && !strings.Contains(s.track, strings.ToLower(f.Track)) {
		return false
	}

	if f.Driver != "" {
		if s.guids[f.Driver] {
			return true
		}

		driver := strings.ToLower(f.Driver)

		for _, name := range s.drivers {
			if strings.Contains(name, driver) {
				return true
			}
		}

		return false
	}

	return true
}

// summaryCache avoids reparsing every results file for each filtered request. Entries are keyed by file
// path and are only valid while the size and mod time of the file are unchanged.
var summaryCache = struct {
	sync.Mutex
	entries map[string]*resultSummary
}{entries: make(map[string]*resultSummary)}

func (l *Loader) prettify(name string) string {
	if l.PrettifyName == nil {
		return name
	}

	return l.PrettifyName(name)
}

func (l *Loader) getResultSummary(info os.FileInfo) (*resultSummary, error) {
	path := filepath.Join(l.Dir, info.Name())

	summaryCache.Lock()
	summary, ok := summaryCache.entries[path]
	summaryCache.Unlock()

	if ok && summary.size == info.Size() && summary.modTime.Equal(info.ModTime()) {
		return summary, nil
	}

	result, err := l.Load(info.Name(), WithoutPluginFire)

	if err != nil {
		return nil, err
	}

	summary = &resultSummary{
		size:    info.Size(),
		modTime: info.ModTime(),
		track: strings.ToLower(strings.Join([]string{
			result.TrackName, l.prettify(result.TrackName), result.TrackConfig, l.prettify(result.TrackConfig),
		}, " ")),
		guids: make(map[string]bool),
	}

	for _, car := range result.Cars {
		summary.drivers = append(summary.drivers, strings.ToLower(car.Driver.Name))
		summary.guids[car.Driver.GUID] = true

		for _, guid := range car.Driver.GuidsList {
			summary.guids[guid] = true
		}
	}

	for _, driver := range result.Result {
		summary.drivers = append(summary.drivers, strings.ToLower(driver.DriverName))
		summary.guids[driver.DriverGUID] = true
	}

	summaryCache.Lock()
	summaryCache.entries[path] = summary
	summaryCache.Unlock()

	return summary, nil
}

// List returns one page of the results, newest first, and the available page numbers.
func (l *Loader) List(page int, filter Filter) ([]SessionResults, []int, error) {
	resultsPath := l.Dir
	resultFiles, err := ioutil.ReadDir(resultsPath)

	if err != nil {
		return nil, nil, err
	}

	if filter.Type != "" {
		var typeFiltered []os.FileInfo

		for _, resultFile := range resultFiles {
			if filter.matchesFileName(resultFile.Name()) {
				typeFiltered = append(typeFiltered, resultFile)
			}
		}

		resultFiles = typeFiltered
	}

	sort.Slice(resultFiles, func(i, j int) bool {
		d1, _ := GetResultDate(resultFiles[i].Name())
		d2, _ := GetResultDate(resultFiles[j].Name())

		return d1.After(d2)
	})

	if filter.needsLoading() {
		var matching []os.FileInfo

		for _, resultFile := range resultFiles {
			summary, err := l.getResultSummary(resultFile)

			if err != nil {
				logrus.WithError(err).Errorf("Could not load results file: %s", resultFile.Name())
				continue
			}

			if summary.matches(filter) {
				matching = append(matching, resultFile)
			}
		}

		resultFiles = matching
	}

	pages := float64(len(resultFiles)) / float64(PageSize)
	pagesRound := math.Ceil(pages)

	// page 0 is always valid, so that an empty results list can still be rendered
	if page < 0 || (page > 0 && page >= int(pagesRound)) {
		return nil, nil, ErrPageNotFound
	}

	var pagesSlice []int

	for x := 0; x < int(pagesRound); x++ {
		pagesSlice = append(pagesSlice, x)
	}

	// get result files for selected page probably
	if len(resultFiles) > page*PageSize+PageSize {
		resultFiles = resultFiles[page*PageSize : page*PageSize+PageSize]
	} else {
		resultFiles = resultFiles[page*PageSize:]
	}

	var results []SessionResults

	for _, resultFile := range resultFiles {
		result, err := l.Load(resultFile.Name())

		if err != nil {
			logrus.WithError(err).Errorf("Could not load results file: %s", resultFile.Name())
			continue
		}

		results = append(results, *result)
	}

	return results, pagesSlice, nil
}

// ListAll returns all results without firing the OnLoad hook, newest first.
func (l *Loader) ListAll() ([]SessionResults, error) {
	resultsPath := l.Dir
	resultFiles, err := ioutil.ReadDir(resultsPath)

	if err != nil {
		return nil, err
	}

	sort.Slice(resultFiles, func(i, j int) bool {
		d1, _ := GetResultDate(resultFiles[i].Name())
		d2, _ := GetResultDate(resultFiles[j].Name())

		return d1.After(d2)
	})

	var results []SessionResults

	for _, resultFile := range resultFiles {
		result, err := l.Load(resultFile.Name(), WithoutPluginFire)

		if err != nil {
			logrus.WithError(err).Errorf("Could not load results file: %s", resultFile.Name())
			continue
		}

		results = append(results, *result)
	}

	return results, nil
}

// ListRecent returns the newest n results, newest first. Results that can't be loaded are skipped.
func (l *Loader) ListRecent(n int) ([]*SessionResults, error) {
	resultFiles, err := ioutil.ReadDir(l.Dir)

	if err != nil {
		return nil, err
	}

	sort.Slice(resultFiles, func(i, j int) bool {
		d1, _ := GetResultDate(resultFiles[i].Name())
		d2, _ := GetResultDate(resultFiles[j].Name())

		return d1.After(d2)
	})

	var results []*SessionResults

	for _, resultFile := range resultFiles {
		if len(results) >= n {
			break
		}

		if resultFile.IsDir() || filepath.Ext(resultFile.Name()) != ".json" {
			continue
		}

		result, err := l.Load(resultFile.Name(), WithoutPluginFire)

		if err != nil {
			logrus.WithError(err).Errorf("Could not load results file: %s", resultFile.Name())
			continue
		}

		results = append(results, result)
	}

	return results, nil
}

// GetResultDate parses the date of a results file from its file name.
func GetResultDate(name string) (time.Time, error) {
	dateSplit := strings.Split(name, "_")
	dateSplit = dateSplit[0 : len(dateSplit)-1]
	date := strings.Join(dateSplit, "_")

	var year, month, day, hour, minute int

	_, err := fmt.Sscanf(date, "%d_%d_%d_%d_%d", &year, &month, &day, &hour, &minute)

	if err != nil {
		return time.Time{}, err
	}

	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.Local), nil
}
