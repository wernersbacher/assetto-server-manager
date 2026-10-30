// Package stats derives statistics (track records, driver profiles, leaderboards) from the results files. The index is
// never persisted: it is rebuilt from the files and refreshed incrementally.
package stats

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/results"
)

// DefaultRefreshInterval is the minimum time between two scans of the results directories done by queries.
const DefaultRefreshInterval = 5 * time.Second

// Index is the statistics index over the results files of a results.Loader. All methods are safe for concurrent use.
type Index struct {
	loader      *results.Loader
	minInterval time.Duration

	// refreshMu serialises refreshes, mu guards the indexed data.
	refreshMu   sync.Mutex
	lastRefresh time.Time

	mu     sync.RWMutex
	files  map[string]*fileStats // keyed by directory and file name
	parses int                   // number of parsed results files, for tests
}

// New returns an index over the results of the loader. Queries refresh the index at most every minInterval (see
// DefaultRefreshInterval); with a zero interval every query scans the directories (only changed files are parsed again).
func New(loader *results.Loader, minInterval time.Duration) *Index {
	return &Index{
		loader:      loader,
		minInterval: minInterval,
		files:       make(map[string]*fileStats),
	}
}

// Refresh scans the results directories now. Files that are new or whose size or modification time changed are
// parsed, entries of removed files are dropped. Files that cannot be read are skipped (and retried on the next
// refresh).
func (i *Index) Refresh() error {
	i.refreshMu.Lock()
	defer i.refreshMu.Unlock()

	return i.refresh()
}

// maybeRefresh refreshes the index unless that already happened within the minimum interval.
func (i *Index) maybeRefresh() {
	i.refreshMu.Lock()
	defer i.refreshMu.Unlock()

	if !i.lastRefresh.IsZero() && time.Since(i.lastRefresh) < i.minInterval {
		return
	}

	if err := i.refresh(); err != nil {
		logrus.WithError(err).Error("Could not refresh the results statistics")
	}
}

// refresh does the work of Refresh, refreshMu must be held.
func (i *Index) refresh() error {
	entries, err := i.loader.Files()

	if err != nil {
		return err
	}

	i.mu.RLock()
	old := i.files
	i.mu.RUnlock()

	next := make(map[string]*fileStats, len(entries))
	parsed := 0

	for _, entry := range entries {
		key := filepath.Join(entry.Dir, entry.Info.Name())

		if prev, ok := old[key]; ok && prev.size == entry.Info.Size() && prev.modTime.Equal(entry.Info.ModTime()) {
			next[key] = prev
			continue
		}

		result, err := i.loader.Load(entry.Info.Name(), results.WithoutPluginFire)
		parsed++

		if err != nil {
			logrus.WithError(err).Errorf("Could not load results file for statistics: %s", entry.Info.Name())
			continue
		}

		next[key] = buildFileStats(result, entry.Info.Name(), entry.Dir, entry.Info.Size(), entry.Info.ModTime())
	}

	i.mu.Lock()
	i.files = next
	i.parses += parsed
	i.mu.Unlock()

	i.lastRefresh = time.Now()

	return nil
}

// snapshot returns the indexed files after a (rate limited) refresh. The returned values are immutable.
func (i *Index) snapshot() []*fileStats {
	i.maybeRefresh()

	i.mu.RLock()
	defer i.mu.RUnlock()

	files := make([]*fileStats, 0, len(i.files))

	for _, f := range i.files {
		files = append(files, f)
	}

	return files
}

// sessionTypeMatches tells if the session type (e.g. RACE, RACEx2) is one of the wanted ones, case-insensitive.
// A wanted "race" also matches the second race of a reversed grid.
func sessionTypeMatches(sessionType string, want []string) bool {
	if len(want) == 0 {
		return true
	}

	for _, w := range want {
		if strings.EqualFold(w, sessionType) || (strings.EqualFold(w, "RACE") && isRace(sessionType)) {
			return true
		}
	}

	return false
}

// isRace tells if the session type is a race (including RACEx2).
func isRace(sessionType string) bool {
	return strings.HasPrefix(strings.ToUpper(sessionType), "RACE")
}
