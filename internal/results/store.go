package results

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// LoadOpts are options for Loader.Load.
type LoadOpts int

// WithoutPluginFire skips the OnLoad hook of the Loader.
const WithoutPluginFire LoadOpts = 0

// Loader reads, lists and writes the AC result files in Dir and ExtraDirs.
type Loader struct {
	// Dir is the primary directory containing the results files. New files are saved here. If a file name exists in
	// Dir and in an extra directory, the file in Dir wins.
	Dir string

	// ExtraDirs are further results directories (e.g. of other server slots) which are loaded and listed together
	// with Dir, in this order. Save and Remove act on the directory that already holds the file. A file name which
	// exists in several directories is only listed once, from the first directory. Extra directories which do not
	// exist are ignored.
	ExtraDirs []string

	// FallBackSorting applies SessionResults.FallBackSort to every loaded result.
	FallBackSorting bool

	// OnLoad is an optional hook (the results Lua plugin) which may replace the loaded results. It is not
	// called if Load is given WithoutPluginFire. Errors are logged and the unmodified results are returned.
	OnLoad func(results *SessionResults) error

	// PrettifyName turns track names and layouts into their readable form for the track filter. Optional.
	PrettifyName func(name string) string
}

// Load reads a single results file from the results directories.
func (l *Loader) Load(fileName string, opts ...LoadOpts) (*SessionResults, error) {
	var result *SessionResults

	data, err := ioutil.ReadFile(l.path(fileName))

	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, &result)

	if err != nil {
		return nil, err
	}

	date, err := GetResultDate(fileName)

	if err == nil {
		result.Date = date
	} else {
		logrus.WithError(err).Errorf("Could not parse results date from filename: %s. Using mod time.", fileName)

		f, err := os.Stat(l.path(fileName))

		if err != nil {
			return nil, err
		}

		result.Date = f.ModTime()
	}

	result.SessionFile = strings.Trim(fileName, ".json")

	var validResults []*SessionResult

	// filter out invalid results
	for _, driver := range result.Result {
		if driver.TotalTime > 0 {
			validResults = append(validResults, driver)
		}
	}

	result.Result = validResults

	if l.FallBackSorting {
		result.FallBackSort()
	}

	var skipLua bool

	for _, opt := range opts {
		if opt == WithoutPluginFire {
			skipLua = true
		}
	}

	if !skipLua && l.OnLoad != nil {
		err = l.OnLoad(result)

		if err != nil {
			logrus.WithError(err).Error("results load plugin script failed")
		}
	}

	return result, nil
}

// ErrInvalidFileName is returned for results file names that could reach files outside of the results directory.
var ErrInvalidFileName = errors.New("servermanager: invalid results file name")

// ValidFileName checks that fileName is a plain base name ending in .json, so that it
// cannot be used to reach files outside of the results directory.
func ValidFileName(fileName string) bool {
	return fileName != "" &&
		strings.HasSuffix(fileName, ".json") &&
		fileName != ".json" &&
		filepath.Base(fileName) == fileName &&
		!strings.ContainsAny(fileName, `/\`)
}

// Remove deletes a results file from the results directories. guard, if not nil, is called after the file was found
// to be a regular file and before it is removed; if it returns an error the file is kept.
func (l *Loader) Remove(fileName string, guard func() error) error {
	if !ValidFileName(fileName) {
		return ErrInvalidFileName
	}

	path := l.path(fileName)

	info, err := os.Stat(path)

	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return ErrInvalidFileName
	}

	if guard != nil {
		if err := guard(); err != nil {
			return err
		}
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	summaryCache.Lock()
	delete(summaryCache.entries, path)
	summaryCache.Unlock()

	return nil
}

// Save takes a full json filepath (including the json extension) and saves the results to that file.
func (l *Loader) Save(jsonFileName string, results *SessionResults) error {
	path := l.path(jsonFileName)

	file, err := os.Create(path)

	if err != nil {
		return err
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "\t")

	return encoder.Encode(results)
}

// ClearSummaryCache drops all cached result summaries.
func ClearSummaryCache() {
	summaryCache.Lock()
	summaryCache.entries = make(map[string]*resultSummary)
	summaryCache.Unlock()
}

// dirs returns all results directories, the primary one first.
func (l *Loader) dirs() []string {
	return append([]string{l.Dir}, l.ExtraDirs...)
}

// path returns the path of a results file: in the first directory that holds it, or in Dir if there is none.
func (l *Loader) path(fileName string) string {
	for _, dir := range l.dirs() {
		path := filepath.Join(dir, fileName)

		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return filepath.Join(l.Dir, fileName)
}

// DirOf returns the directory which holds the results file, Dir if no directory does. It is how a result is
// attributed to the server slot which produced it.
func (l *Loader) DirOf(fileName string) string {
	return filepath.Dir(l.path(fileName))
}

// inDir tells if the results file is found in the directory dir.
func (l *Loader) inDir(fileName, dir string) bool {
	return filepath.Clean(l.DirOf(fileName)) == filepath.Clean(dir)
}

// HeldByOtherDir tells if the results file exists in one of ExtraDirs but not in Dir. Saving a new file of the same
// name to Dir would then create a copy which the other one hides.
func (l *Loader) HeldByOtherDir(fileName string) bool {
	if _, err := os.Stat(filepath.Join(l.Dir, fileName)); err == nil {
		return false
	}

	_, err := os.Stat(l.path(fileName))

	return err == nil
}
