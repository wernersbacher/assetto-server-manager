package servermanager

import (
	"context"

	"github.com/wernersbacher/ACPitwall/internal/carclasses"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
)

// resolveCarClassManager returns the manager of the global car classes. Like the teams and skins it is shared by all
// slots: car classes and championships are shared, and the manager serialises its changes with the saving of
// championships of every slot (championship.LockCarClassChanges).
func (r *Resolver) resolveCarClassManager() *carclasses.Manager {
	if r.carClassManager != nil {
		return r.carClassManager
	}

	r.carClassManager = carclasses.NewManager(r.store)

	return r.carClassManager
}

// resolveCarClassHandler returns the administration pages of the car classes. A class can be made of the cars the
// race builder offers (everything the car search index knows).
func (r *Resolver) resolveCarClassHandler() *carclasses.Handler {
	if r.carClassHandler != nil {
		return r.carClassHandler
	}

	cars := r.resolveCarManager()

	r.carClassHandler = carclasses.NewHandler(r.resolveBaseHandler(), r.resolveCarClassManager(), func(ctx context.Context) (smcontent.Cars, error) {
		_, found, err := cars.Search(ctx, "", 0, 100000)

		return found, err
	})

	return r.carClassHandler
}
