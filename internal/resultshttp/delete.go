package resultshttp

import (
	"fmt"
	"strings"

	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// ResultsFileReferencedError is returned when a results file can't be deleted because a championship or race weekend uses it.
type ResultsFileReferencedError struct {
	// Name is the name of the championship (Championship is true) or race weekend which uses the file.
	Name         string
	Championship bool
}

// Reference describes what uses the file, e.g. "championship Foo".
func (e ResultsFileReferencedError) Reference() string {
	if e.Championship {
		return "championship " + e.Name
	}

	return "race weekend " + e.Name
}

func (e ResultsFileReferencedError) Error() string {
	return fmt.Sprintf("servermanager: results file is referenced by %s", e.Reference())
}

// findResultsReference returns the championship or race weekend which uses the given results file, or nil if it is
// not referenced. Championship events and race weekend sessions embed a copy of the results they were built from, so
// they are matched by the results' SessionFile.
func findResultsReference(store smstore.Store, fileName string) (*ResultsFileReferencedError, error) {
	// LoadResult derives SessionFile from the file name in this way
	sessionFile := strings.Trim(fileName, ".json")

	championships, err := store.ListChampionships()

	if err != nil {
		return nil, err
	}

	for _, championship := range championships {
		for _, event := range championship.Events {
			for _, session := range event.Sessions {
				if session.Results != nil && session.Results.SessionFile == sessionFile {
					return &ResultsFileReferencedError{Name: championship.Name, Championship: true}, nil
				}

				if session.RaceWeekendSession != nil && session.RaceWeekendSession.Results != nil && session.RaceWeekendSession.Results.SessionFile == sessionFile {
					return &ResultsFileReferencedError{Name: championship.Name, Championship: true}, nil
				}
			}
		}
	}

	raceWeekends, err := store.ListRaceWeekends()

	if err != nil {
		return nil, err
	}

	for _, raceWeekend := range raceWeekends {
		for _, session := range raceWeekend.Sessions {
			if session.Results != nil && session.Results.SessionFile == sessionFile {
				return &ResultsFileReferencedError{Name: raceWeekend.Name}, nil
			}
		}
	}

	return nil, nil
}

// deleteResultsFile removes a results file from the results directory, unless it is used by a championship or race weekend.
func deleteResultsFile(env *serverenv.Env, store smstore.Store, fileName string) error {
	return env.Results().Remove(fileName, func() error {
		reference, err := findResultsReference(store, fileName)

		if err != nil {
			return err
		}

		if reference != nil {
			return *reference
		}

		return nil
	})
}
