package servermanager

import (
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

func (rm *RaceManager) LoopRaces() {
	var i int
	ticker := time.NewTicker(30 * time.Second)

	for range ticker.C {
		currentRace, _ := rm.CurrentRace()

		if currentRace != nil {
			continue
		}

		_, _, looped, _, err := rm.ListCustomRaces()

		if err != nil {
			logrus.WithError(err).Errorf("couldn't list custom races")
			return
		}

		if looped != nil {
			if i >= len(looped) {
				i = 0
			}

			// Reset the stored session types
			rm.loopedRaceSessionTypes = []serverconfig.SessionType{}

			for sessionID := range looped[i].RaceConfig.Sessions {
				rm.loopedRaceSessionTypes = append(rm.loopedRaceSessionTypes, sessionID)
			}

			if looped[i].RaceConfig.ReversedGridRacePositions != 0 {
				rm.loopedRaceSessionTypes = append(rm.loopedRaceSessionTypes, serverconfig.SessionTypeSecondRace)
			}

			_, err := rm.StartCustomRace(looped[i].UUID.String(), true)

			if err != nil {
				logrus.WithError(err).Errorf("couldn't start auto loop custom race")
				return
			}

			i++
		}
	}
}

// callback check for udp end session, load result file, check session type against sessionTypes
// if session matches last session in sessionTypes then stop server and clear sessionTypes
func (rm *RaceManager) LoopCallback(message udp.Message) {
	if a, ok := message.(udp.EndSession); ok {
		if rm.loopedRaceSessionTypes == nil {
			logrus.Infof("Session types == nil. ignoring end session callback")
			return
		}

		filename := filepath.Base(string(a))

		results, err := LoadResult(filename)

		if err != nil {
			logrus.WithError(err).Errorf("Could not read session results for %s", filename)
			return
		}

		var endSession serverconfig.SessionType

		// If this is a race, and there is a second race configured
		// then wait for the second race to happen.
		if results.Type == serverconfig.SessionTypeRace {
			for _, session := range rm.loopedRaceSessionTypes {
				if session == serverconfig.SessionTypeSecondRace {
					if !rm.loopedRaceWaitForSecondRace {
						rm.loopedRaceWaitForSecondRace = true
						return
					}

					rm.loopedRaceWaitForSecondRace = false
				}
			}
		}

		for _, session := range rm.loopedRaceSessionTypes {
			if session == serverconfig.SessionTypeRace {
				endSession = serverconfig.SessionTypeRace
				break
			} else if session == serverconfig.SessionTypeQualifying {
				endSession = serverconfig.SessionTypeQualifying
			} else if session == serverconfig.SessionTypePractice && endSession != serverconfig.SessionTypeQualifying {
				endSession = serverconfig.SessionTypePractice
			} else if session == serverconfig.SessionTypeBooking && (endSession != serverconfig.SessionTypeQualifying && endSession != serverconfig.SessionTypePractice) {
				endSession = serverconfig.SessionTypeBooking
			}
		}

		logrus.Infof("results type: %s, endSession: %s", results.Type, string(endSession))

		if results.Type == endSession {
			logrus.Infof("Event end detected, stopping looped session.")

			rm.clearLoopedRaceSessionTypes()

			err := rm.process.Stop()

			if err != nil {
				logrus.WithError(err).Errorf("Could not stop server")
				return
			}
		}
	}
}

func (rm *RaceManager) clearLoopedRaceSessionTypes() {
	rm.loopedRaceSessionTypes = nil
}
