package servermanager

import (
	"fmt"
	"path/filepath"
	"strings"

	smlua "github.com/wernersbacher/assetto-server-manager/internal/lua"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

// UseFallBackSorting makes loaded results use SessionResults.FallBackSort. It is set from the server options.
var UseFallBackSorting = false

// LoadResultOpts are options for LoadResult.
type LoadResultOpts = smresults.LoadOpts

// LoadResultWithoutPluginFire skips the results Lua plugin.
const LoadResultWithoutPluginFire = smresults.WithoutPluginFire

var (
	// ErrResultsPageNotFound is returned by listResults for pages that don't exist.
	ErrResultsPageNotFound = smresults.ErrPageNotFound

	// ErrInvalidResultsFileName is returned for results file names that point outside of the results directory.
	ErrInvalidResultsFileName = smresults.ErrInvalidFileName
)

const (
	pageSize         = smresults.PageSize
	timeAttackSuffix = smresults.TimeAttackSuffix
)

// resultsLoader builds a smresults.Loader from the current installation path and server options.
func resultsLoader() *smresults.Loader {
	loader := &smresults.Loader{
		Dir:             filepath.Join(ServerInstallPath, "results"),
		FallBackSorting: UseFallBackSorting,
		PrettifyName:    func(name string) string { return prettifyName(name, false) },
	}

	if smlua.Initialised() {
		loader.OnLoad = resultsLoadPlugin
	}

	return loader
}

func LoadResult(fileName string, opts ...LoadResultOpts) (*smresults.SessionResults, error) {
	return resultsLoader().Load(fileName, opts...)
}

func listResults(page int, filter smresults.Filter) ([]smresults.SessionResults, []int, error) {
	return resultsLoader().List(page, filter)
}

func ListAllResults() ([]smresults.SessionResults, error) {
	return resultsLoader().ListAll()
}

func listRecentResults(n int) ([]*smresults.SessionResults, error) {
	return resultsLoader().ListRecent(n)
}

func saveResults(jsonFileName string, results *smresults.SessionResults) error {
	return resultsLoader().Save(jsonFileName, results)
}

func resultsLoadPlugin(results *smresults.SessionResults) error {
	p := smlua.NewPlugin()

	newSessionResults := &smresults.SessionResults{}

	p.Inputs(results).Outputs(newSessionResults)
	err := p.Call("./plugins/results.lua", "onResultsLoad")

	if err != nil {
		return err
	}

	*results = *newSessionResults

	return nil
}

// ResultsFileReferencedError is returned when a results file can't be deleted because a championship or race weekend uses it.
type ResultsFileReferencedError struct {
	Reference string
}

func (e ResultsFileReferencedError) Error() string {
	return fmt.Sprintf("servermanager: results file is referenced by %s", e.Reference)
}

// findResultsReference returns a description of the championship or race weekend which uses the given results file,
// or an empty string if it is not referenced. Championship events and race weekend sessions embed a copy of the
// results they were built from, so they are matched by the results' SessionFile.
func findResultsReference(store smstore.Store, fileName string) (string, error) {
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
func deleteResultsFile(store smstore.Store, fileName string) error {
	return resultsLoader().Remove(fileName, func() error {
		reference, err := findResultsReference(store, fileName)

		if err != nil {
			return err
		}

		if reference != "" {
			return ResultsFileReferencedError{Reference: reference}
		}

		return nil
	})
}
