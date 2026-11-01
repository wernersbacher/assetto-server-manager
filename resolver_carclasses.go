package servermanager

import "github.com/wernersbacher/ACPitwall/internal/carclasses"

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
