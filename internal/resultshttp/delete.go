package resultshttp

import (
	"fmt"
	"strings"

	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

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
func deleteResultsFile(env *serverenv.Env, store smstore.Store, fileName string) error {
	return env.Results().Remove(fileName, func() error {
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
