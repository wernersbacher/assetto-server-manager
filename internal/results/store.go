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

// Loader reads, lists and writes the AC result files in Dir.
type Loader struct {
	// Dir is the directory containing the results files.
	Dir string

	// FallBackSorting applies SessionResults.FallBackSort to every loaded result.
	FallBackSorting bool

	// OnLoad is an optional hook (the results Lua plugin) which may replace the loaded results. It is not
	// called if Load is given WithoutPluginFire. Errors are logged and the unmodified results are returned.
	OnLoad func(results *SessionResults) error

	// PrettifyName turns track names and layouts into their readable form for the track filter. Optional.
	PrettifyName func(name string) string
}

// Load reads a single results file from the results directory.
func (l *Loader) Load(fileName string, opts ...LoadOpts) (*SessionResults, error) {
	var result *SessionResults

	data, err := ioutil.ReadFile(filepath.Join(l.Dir, fileName))

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

		f, err := os.Stat(filepath.Join(l.Dir, fileName))

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

// Remove deletes a results file from the results directory. guard, if not nil, is called after the file was found
// to be a regular file and before it is removed; if it returns an error the file is kept.
func (l *Loader) Remove(fileName string, guard func() error) error {
	if !ValidFileName(fileName) {
		return ErrInvalidFileName
	}

	path := filepath.Join(l.Dir, fileName)

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
	path := filepath.Join(l.Dir, jsonFileName)

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
