package servermanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

var UseFallBackSorting = false

type LoadResultOpts int

const LoadResultWithoutPluginFire LoadResultOpts = 0

func LoadResult(fileName string, opts ...LoadResultOpts) (*SessionResults, error) {
	var result *SessionResults

	resultsPath := filepath.Join(ServerInstallPath, "results")

	data, err := ioutil.ReadFile(filepath.Join(resultsPath, fileName))

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

		f, err := os.Stat(filepath.Join(resultsPath, fileName))

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

	if UseFallBackSorting {
		result.FallBackSort()
	}

	var skipLua bool

	for _, opt := range opts {
		if opt == LoadResultWithoutPluginFire {
			skipLua = true
		}
	}

	if !skipLua && luaInitialised {
		err = resultsLoadPlugin(result)

		if err != nil {
			logrus.WithError(err).Error("results load plugin script failed")
		}
	}

	return result, nil
}

func resultsLoadPlugin(results *SessionResults) error {
	p := NewLuaPlugin()

	newSessionResults := &SessionResults{}

	p.Inputs(results).Outputs(newSessionResults)
	err := p.Call("./plugins/results.lua", "onResultsLoad")

	if err != nil {
		return err
	}

	*results = *newSessionResults

	return nil
}

var ErrInvalidResultsFileName = errors.New("servermanager: invalid results file name")

// ResultsFileReferencedError is returned when a results file can't be deleted because a championship or race weekend uses it.
type ResultsFileReferencedError struct {
	Reference string
}

func (e ResultsFileReferencedError) Error() string {
	return fmt.Sprintf("servermanager: results file is referenced by %s", e.Reference)
}

// validResultsFileName checks that fileName is a plain base name ending in .json, so that it
// cannot be used to reach files outside of the results directory.
func validResultsFileName(fileName string) bool {
	return fileName != "" &&
		strings.HasSuffix(fileName, ".json") &&
		fileName != ".json" &&
		filepath.Base(fileName) == fileName &&
		!strings.ContainsAny(fileName, `/\`)
}

// findResultsReference returns a description of the championship or race weekend which uses the given results file,
// or an empty string if it is not referenced. Championship events and race weekend sessions embed a copy of the
// results they were built from, so they are matched by the results' SessionFile.
func findResultsReference(store Store, fileName string) (string, error) {
	// LoadResult derives SessionFile from the file name in this way
	sessionFile := strings.Trim(fileName, ".json")

	championships, err := store.ListChampionships()

	if err != nil {
		return "", err
	}

	for _, championship := range championships {
		for _, event := range championship.Events {
			for _, session := range event.Sessions {
				if session.Results != nil && session.Results.SessionFile == sessionFile {
					return fmt.Sprintf("championship %s", championship.Name), nil
				}

				if session.RaceWeekendSession != nil && session.RaceWeekendSession.Results != nil && session.RaceWeekendSession.Results.SessionFile == sessionFile {
					return fmt.Sprintf("championship %s", championship.Name), nil
				}
			}
		}
	}

	raceWeekends, err := store.ListRaceWeekends()

	if err != nil {
		return "", err
	}

	for _, raceWeekend := range raceWeekends {
		for _, session := range raceWeekend.Sessions {
			if session.Results != nil && session.Results.SessionFile == sessionFile {
				return fmt.Sprintf("race weekend %s", raceWeekend.Name), nil
			}
		}
	}

	return "", nil
}

// deleteResultsFile removes a results file from the results directory, unless it is used by a championship or race weekend.
func deleteResultsFile(store Store, fileName string) error {
	if !validResultsFileName(fileName) {
		return ErrInvalidResultsFileName
	}

	path := filepath.Join(ServerInstallPath, "results", fileName)

	info, err := os.Stat(path)

	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return ErrInvalidResultsFileName
	}

	reference, err := findResultsReference(store, fileName)

	if err != nil {
		return err
	}

	if reference != "" {
		return ResultsFileReferencedError{Reference: reference}
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	resultSummaryCache.Lock()
	delete(resultSummaryCache.entries, fileName)
	resultSummaryCache.Unlock()

	return nil
}

// saveResults takes a full json filepath (including the json extension) and saves the results to that file.
func saveResults(jsonFileName string, results *SessionResults) error {
	path := filepath.Join(ServerInstallPath, "results", jsonFileName)

	file, err := os.Create(path)

	if err != nil {
		return err
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "\t")

	return encoder.Encode(results)
}
