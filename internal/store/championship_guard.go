package store

import (
	"sync"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// carClassMutex orders changes to the global car classes against the saving of championships: every save through
// UpsertChampionshipChecked holds it for reading, LockCarClassWrites holds it for writing. It lives here, below all
// packages that save championships (championship, raceweekend, racecontrol), so that all of them can take part.
var carClassMutex sync.RWMutex

// LockCarClassWrites stops all saves through UpsertChampionshipChecked until the returned function is called. Whoever
// changes a global car class takes it, after the registrations lock of the championship package (the order of a sign
// up: registrations, then save).
func LockCarClassWrites() func() {
	carClassMutex.Lock()

	return carClassMutex.Unlock
}

// UpsertChampionshipChecked saves the championship like Store.UpsertChampionship. If it refers to global car classes,
// they are checked again against the car classes as they are now (Championship.CheckCarClasses; the violation is
// returned as the i18n.Message of the model), and the save is serialised with changes to car classes: a championship
// that was loaded or validated before a car class changed cannot be saved into an invalid state, and a car class
// change always sees the championships as they were saved before it.
//
// Every code path that writes a championship it loaded earlier (also race control for penalties and results, and the
// race weekend manager) must save through this function, because a stale copy would otherwise overwrite what a car
// class change has just checked. Callers must not hold the lock (it is not reentrant: a waiting car class change would
// block a second read lock), so never call it from code that runs inside UpsertChampionshipChecked.
func UpsertChampionshipChecked(store Store, c *smmodel.Championship) error {
	carClassMutex.RLock()
	defer carClassMutex.RUnlock()

	if c.HasCarClassReferences() {
		carClasses, err := store.ListCarClasses()

		if err != nil {
			return err
		}

		lookup := smmodel.NewCarClassMap(carClasses)

		// the classes of a form do not have a lookup yet, so that the cars of references are resolved from the store as it is now
		c.SetCarClassLookup(lookup)

		if err := c.CheckCarClasses(lookup); err != nil {
			return err
		}
	}

	return store.UpsertChampionship(c)
}
