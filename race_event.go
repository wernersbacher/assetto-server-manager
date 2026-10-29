package servermanager

import (
	"fmt"

	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

func describeRaceEvent(raceEvent smmodel.RaceEvent) string {
	cfg := raceEvent.GetRaceConfig()

	return fmt.Sprintf("%d sessions with %d entrants at %s (%s)", len(cfg.Sessions), len(raceEvent.GetEntryList()), cfg.Track, cfg.TrackLayout)
}
