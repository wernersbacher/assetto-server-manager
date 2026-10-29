package model

import (
	"fmt"
	"html/template"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

// DefaultChampionshipPoints is the Formula 1 points system.
var DefaultChampionshipPoints = ChampionshipPoints{
	Places: []int{
		25,
		18,
		15,
		12,
		10,
		8,
		6,
		4,
		2,
		1,
	},
	BestLap:              0,
	PolePosition:         0,
	SecondRaceMultiplier: 1,

	CollisionWithDriver: 0,
	CollisionWithEnv:    0,
	CutTrack:            0,
}

// ChampionshipPoints represent the potential points for positions as well as other awards in a Championship.
type ChampionshipPoints struct {
	Places       []int
	BestLap      int
	PolePosition int

	CollisionWithDriver int
	CollisionWithEnv    int
	CutTrack            int

	SecondRaceMultiplier float64
}

// PointForPos uses the Championship's Points to determine what number should be awarded to a given position
func (pts *ChampionshipPoints) ForPos(i int) float64 {
	if i >= len(pts.Places) {
		return 0
	}

	return float64(pts.Places[i])
}

func (c *ChampionshipClass) HadBetterFinishingPositions(guidA, guidB string, inEvents []*ChampionshipEvent, numPositions int) bool {
	for i := 1; i <= numPositions; i++ {
		countDriverA := c.CountPositionsForDriver(i, guidA, inEvents)
		countDriverB := c.CountPositionsForDriver(i, guidB, inEvents)

		if countDriverA == countDriverB {
			continue
		}

		return countDriverA > countDriverB
	}

	return false
}

// forEachCompletedRace calls fn with the results of every completed race (and second race) session in the given
// events. The sessions of race weekends are treated as individual events, like when calculating the standings.
func forEachCompletedRace(inEvents []*ChampionshipEvent, fn func(results *smresults.SessionResults)) {
	for _, event := range ExtractRaceWeekendSessionsIntoIndividualEvents(inEvents) {
		for sessionType, session := range event.Sessions {
			if session.Completed() && (sessionType == serverconfig.SessionTypeRace || sessionType == serverconfig.SessionTypeSecondRace) {
				fn(session.Results)
			}
		}
	}
}

// numFinishingPositions is the highest number of finishers of this class in any completed race of the given events,
// i.e. the number of positions that need to be compared in a tie-break.
func (c *ChampionshipClass) numFinishingPositions(inEvents []*ChampionshipEvent) int {
	numPositions := 0

	forEachCompletedRace(inEvents, func(results *smresults.SessionResults) {
		num := 0

		for _, result := range results.Result {
			if result.ClassID == c.ID {
				num++
			}
		}

		if num > numPositions {
			numPositions = num
		}
	})

	return numPositions
}

func (c *ChampionshipClass) CountPositionsForDriver(i int, guid string, inEvents []*ChampionshipEvent) int {
	count := 0

	forEachCompletedRace(inEvents, func(results *smresults.SessionResults) {
		pos := 0

		for _, result := range results.Result {
			if result.ClassID == c.ID {
				pos++

				if guid == result.DriverGUID && pos == i {
					count++
				}
			}
		}
	})

	return count
}

func (c *ChampionshipClass) TeamHadBetterFinishingPositions(teamA, teamB string, inEvents []*ChampionshipEvent, numPositions int) bool {
	for i := 1; i <= numPositions; i++ {
		countTeamA := c.CountPositionsForTeam(i, teamA, inEvents)
		countTeamB := c.CountPositionsForTeam(i, teamB, inEvents)

		if countTeamA == countTeamB {
			continue
		}

		return countTeamA > countTeamB
	}

	return teamA < teamB
}

func (c *ChampionshipClass) CountPositionsForTeam(i int, team string, inEvents []*ChampionshipEvent) int {
	count := 0

	forEachCompletedRace(inEvents, func(results *smresults.SessionResults) {
		pos := 0

		for _, result := range results.Result {
			if result.ClassID != c.ID {
				continue
			}

			for _, car := range results.Cars {
				if car.Driver.GUID == result.DriverGUID && car.Driver.ClassID == result.ClassID {
					pos++

					if team == car.Driver.Team && pos == i {
						count++
					}

					break
				}
			}
		}
	})

	return count
}

func (c *Championship) FindLastResultForDriver(guid string) (out *smresults.SessionResult, teamName string) {
	events := make([]*ChampionshipEvent, 0)

	for _, event := range ExtractRaceWeekendSessionsIntoIndividualEvents(c.Events) {
		if event.Completed() {
			events = append(events, event)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].CompletedTime.Before(events[j].CompletedTime)
	})

	// the result (and the team the driver was in) of the most recently started session that the driver took part in,
	// in the last completed event that the driver took part in.
	for _, event := range events {
		var latestSession *ChampionshipSession
		var latestResult *smresults.SessionResult

		for _, session := range event.Sessions {
			if session.Results == nil || (latestSession != nil && !session.StartedTime.After(latestSession.StartedTime)) {
				continue
			}

			for _, result := range session.Results.Result {
				if result.DriverGUID == guid {
					latestSession = session
					latestResult = result
					break
				}
			}
		}

		if latestResult != nil {
			out = latestResult
			teamName = latestSession.Results.GetTeamName(guid)
		}
	}

	return out, teamName
}

func (c *Championship) GetPlayerSummary(guid string) string {
	if c.Progress() == 0 {
		if len(c.Events) <= 1 {
			return ""
		}

		return "This is the first event of the Championship!"
	}

	result, teamName := c.FindLastResultForDriver(guid)

	if result == nil {
		return ""
	}

	class, err := c.ClassByID(result.ClassID.String())

	if err != nil {
		logrus.WithError(err).Warnf("Could not find class by id: %s", result.ClassID.String())
		return ""
	}

	standings := class.Standings(c, c.Events)
	teamStandings := class.TeamStandings(c, c.Events)

	var driverPos, teamPos int
	var driverPoints, teamPoints float64

	for pos, standing := range standings {
		if standing.Car.Driver.GUID == guid {
			driverPos = pos + 1
			driverPoints = standing.Points
		}
	}

	var driverAhead string
	var driverAheadPoints float64

	if driverPos >= 2 {
		driverAhead = standings[driverPos-2].Car.Driver.Name
		driverAheadPoints = standings[driverPos-2].Points
	}

	for pos, standing := range teamStandings {
		if standing.Team == teamName {
			teamPos = pos + 1
			teamPoints = standing.Points
		}
	}

	classText := ""

	if class.Name != "" {
		classText = fmt.Sprintf("in the class '%s' ", class.Name)
	}

	out := fmt.Sprintf("You are currently %d%s %swith %.2f points. ", driverPos, Ordinal(int64(driverPos)), classText, driverPoints)

	if driverAhead != "" {
		out += fmt.Sprintf("The driver ahead of you is %s with %.2f points. ", driverAhead, driverAheadPoints)
	}

	if teamName != "" {
		var teamAhead string
		var teamAheadPoints float64

		if teamPos >= 2 {
			teamAhead = teamStandings[teamPos-2].Team
			teamAheadPoints = teamStandings[teamPos-2].Points
		}

		out += fmt.Sprintf("Your team '%s' has %.2f points. ", teamName, teamPoints)

		if teamAhead != "" {
			out += fmt.Sprintf("The team ahead is '%s' with %.2f points. ", teamAhead, teamAheadPoints)
		}
	}

	return out
}

func NewChampionshipStanding(car *smresults.SessionCar) *ChampionshipStanding {
	return &ChampionshipStanding{
		Car:   car,
		Teams: make(map[string]int),
	}
}

// ChampionshipStanding is the current number of Points an Entrant in the Championship has.
type ChampionshipStanding struct {
	Car *smresults.SessionCar

	// Teams is a map of Team Name to how many Events per team were completed.
	Teams  map[string]int
	Points float64

	// DroppedEvents contains the IDs (string form) of the Championship Events whose result was dropped for this
	// driver (see Championship.DropWorstResults). Points already has the dropped results subtracted.
	DroppedEvents map[string]bool
	// DroppedPoints is the sum of the points that were dropped.
	DroppedPoints float64
}

// IsDropped reports whether the result of the given Championship Event is dropped for this standing.
func (cs *ChampionshipStanding) IsDropped(eventID uuid.UUID) bool {
	return cs.DroppedEvents[eventID.String()]
}

// dropContext knows which Championship Events are completed (and therefore droppable) and maps the individual
// sessions of race weekends (which are treated as separate events while calculating points) back to the
// Championship Event they belong to.
type dropContext struct {
	completedEventIDs []string
	owner             map[uuid.UUID]uuid.UUID
}

func newDropContext(inEvents []*ChampionshipEvent) *dropContext {
	ctx := &dropContext{owner: make(map[uuid.UUID]uuid.UUID)}

	for _, event := range inEvents {
		if event.Completed() {
			ctx.completedEventIDs = append(ctx.completedEventIDs, event.ID.String())
		}

		if event.IsRaceWeekend() && event.RaceWeekend != nil {
			for _, session := range event.RaceWeekend.Sessions {
				ctx.owner[session.ID] = event.ID
			}
		}
	}

	return ctx
}

func (d *dropContext) eventID(id uuid.UUID) string {
	if owner, ok := d.owner[id]; ok {
		return owner.String()
	}

	return id.String()
}

// dropWorstResults determines which of the completed events are dropped, given the score per event.
// Events without a score count as 0 and are droppable. At least one result always counts.
func (d *dropContext) dropWorstResults(n int, scores map[string]float64) (dropped map[string]bool, droppedPoints float64) {
	numDrops := n

	if numDrops > len(d.completedEventIDs)-1 {
		numDrops = len(d.completedEventIDs) - 1
	}

	if numDrops <= 0 {
		return nil, 0
	}

	ids := make([]string, len(d.completedEventIDs))
	copy(ids, d.completedEventIDs)

	sort.SliceStable(ids, func(i, j int) bool {
		return scores[ids[i]] < scores[ids[j]]
	})

	dropped = make(map[string]bool)

	for _, id := range ids[:numDrops] {
		dropped[id] = true
		droppedPoints += scores[id]
	}

	return dropped, droppedPoints
}

func (cs *ChampionshipStanding) AddEventForTeam(team string) {
	cs.Teams[team]++
}

func (cs *ChampionshipStanding) TeamSummary() string {
	if len(cs.Teams) == 1 {
		for team := range cs.Teams {
			return team
		}
	} else {
		// more than one team
		var summary []string

		for team, races := range cs.Teams {
			summary = append(summary, fmt.Sprintf("%s (%d races)", team, races))
		}

		return strings.Join(summary, ", ")
	}

	return ""
}

func (c *ChampionshipClass) ResultsForClass(results []*smresults.SessionResult, championship *Championship) (filtered []*smresults.SessionResult) {
	for _, result := range results {
		if c.DriverInClass(result) && result.TotalTime > 0 && (!championship.SpectatorCarEnabled || result.DriverGUID != championship.SpectatorCar.GUID) {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

func (c *ChampionshipClass) PenaltyForGUID(guid string) int {
	if penalty, ok := c.DriverPenalties[guid]; ok {
		return penalty
	}

	return 0
}

func (c *ChampionshipClass) PenaltyForTeam(name string) int {
	if teamPenalty, ok := c.TeamPenalties[name]; ok {
		return teamPenalty
	}

	return 0
}

type PointsReason int

const (
	PointsEventFinish PointsReason = iota
	PointsPolePosition
	PointsFastestLap
	PointsCollisionWithCar
	PointsCollisionWithEnvironment
	PointsCutTrack
)

func (c *ChampionshipClass) standings(championship *Championship, events []*ChampionshipEvent, givePoints func(event *ChampionshipEvent, driverGUID string, points float64, reason PointsReason)) {
	eventsReverseCompletedOrder := make([]*ChampionshipEvent, len(events))

	copy(eventsReverseCompletedOrder, events)

	sort.Slice(eventsReverseCompletedOrder, func(i, j int) bool {
		return eventsReverseCompletedOrder[i].CompletedTime.After(eventsReverseCompletedOrder[j].CompletedTime)
	})

	for _, event := range eventsReverseCompletedOrder {
		for sessionType, session := range event.Sessions {
			if !session.Completed() || session.Results == nil {
				continue
			}

			points := c.Points
			pointsMultiplier := 1.0

			if session.IsRaceWeekend() {
				// race weekend sessions are valid points, as specified by the session itself.
				classPoints, ok := session.RaceWeekendSession.Points[c.ID]

				if !ok {
					logrus.Warnf("Could not find points for Race Weekend Session class: %s", c.ID)
				} else {
					points = *classPoints
				}
			} else {
				switch sessionType {
				case serverconfig.SessionTypeQualifying:
					// non race weekend qualifying results get pole position points
					for pos, driver := range c.ResultsForClass(session.Results.Result, championship) {
						if pos != 0 {
							continue
						}

						givePoints(event, driver.DriverGUID, float64(points.PolePosition)*pointsMultiplier, PointsPolePosition)
					}

					continue

				case serverconfig.SessionTypeSecondRace:
					pointsMultiplier = points.SecondRaceMultiplier
				case serverconfig.SessionTypeBooking, serverconfig.SessionTypePractice:
					continue

				default:
					// race sessions fall through
				}
			}

			fastestLap := session.Results.FastestLapInClass(c.ID)

			for pos, driver := range c.ResultsForClass(session.Results.Result, championship) {
				if driver.TotalTime <= 0 || driver.Disqualified {
					continue
				}

				givePoints(event, driver.DriverGUID, points.ForPos(pos)*pointsMultiplier, PointsEventFinish)

				if fastestLap != nil && fastestLap.DriverGUID == driver.DriverGUID {
					givePoints(event, driver.DriverGUID, float64(points.BestLap)*pointsMultiplier, PointsFastestLap)
				}

				if sessionType == serverconfig.SessionTypeRace || sessionType == serverconfig.SessionTypeSecondRace {
					givePoints(event, driver.DriverGUID, float64(points.CollisionWithDriver*session.Results.GetCrashesOfType(driver.DriverGUID, driver.CarModel, "COLLISION_WITH_CAR"))*pointsMultiplier*-1, PointsCollisionWithCar)
					givePoints(event, driver.DriverGUID, float64(points.CollisionWithEnv*session.Results.GetCrashesOfType(driver.DriverGUID, driver.CarModel, "COLLISION_WITH_ENV"))*pointsMultiplier*-1, PointsCollisionWithEnvironment)
					givePoints(event, driver.DriverGUID, float64(points.CutTrack*session.Results.GetCuts(driver.DriverGUID, driver.CarModel))*pointsMultiplier*-1, PointsCutTrack)
				}
			}
		}
	}
}

var championshipStandingSessionOrder = []serverconfig.SessionType{
	serverconfig.SessionTypeSecondRace,
	serverconfig.SessionTypeRace,
	serverconfig.SessionTypeQualifying,
	serverconfig.SessionTypePractice,
	serverconfig.SessionTypeBooking,
}

type StandingsOption int

const (
	StandingsNoPointsPenalties StandingsOption = iota
)

// Standings returns the current Driver Standings for the Championship.
func (c *ChampionshipClass) Standings(championship *Championship, inEvents []*ChampionshipEvent, standingOpts ...StandingsOption) []*ChampionshipStanding {
	var out []*ChampionshipStanding

	// make a copy of events so we do not persist race weekend sessions
	events := ExtractRaceWeekendSessionsIntoIndividualEvents(inEvents)

	for _, event := range events {
		for _, session := range event.Sessions {
			if session.Results == nil {
				continue
			}

			// sort session result cars by the last car they completed a lap in (reversed).
			// this means that below when we're looking for the car that matches a driver, we find
			// the most recent car that they drove.

			// sorting occurs outside the standings call below for performance reasons.
			sort.Slice(session.Results.Cars, func(i, j int) bool {
				carI := session.Results.Cars[i]
				carJ := session.Results.Cars[j]

				carILastLap := 0
				carJLastLap := 0

				for _, lap := range session.Results.Laps {
					if lap.CarID == carI.CarID && lap.Timestamp > carILastLap {
						carILastLap = lap.Timestamp
					}

					if lap.CarID == carJ.CarID && lap.Timestamp > carJLastLap {
						carJLastLap = lap.Timestamp
					}
				}

				return carILastLap > carJLastLap
			})
		}
	}

	standings := make(map[string]*ChampionshipStanding)
	dropCtx := newDropContext(inEvents)
	eventPoints := make(map[string]map[string]float64)

	c.standings(championship, events, func(event *ChampionshipEvent, driverGUID string, points float64, reason PointsReason) {
		var car *smresults.SessionCar

		for _, sessionType := range championshipStandingSessionOrder {
			session, ok := event.Sessions[sessionType]

			if !ok || session.Results == nil {
				continue
			}

			for _, sessionCar := range session.Results.Cars {
				if sessionCar.Driver.GUID == driverGUID {
					car = sessionCar
					break
				}

				if serverconfig.NormaliseEntrantGUIDs(sessionCar.Driver.GuidsList) == driverGUID {
					car = sessionCar
					break
				}
			}
		}

		if car == nil {
			return
		}

		if _, ok := standings[driverGUID]; !ok {
			standings[driverGUID] = NewChampionshipStanding(car)
		}

		standings[driverGUID].Points += points

		if eventPoints[driverGUID] == nil {
			eventPoints[driverGUID] = make(map[string]float64)
		}

		eventPoints[driverGUID][dropCtx.eventID(event.ID)] += points

		if reason == PointsEventFinish {
			// only increment team finishes for a 'finish' reason
			standings[driverGUID].AddEventForTeam(car.Driver.Team)
		}
	})

	skipPointsPenalties := false

	for _, opt := range standingOpts {
		if opt == StandingsNoPointsPenalties {
			skipPointsPenalties = true
		}
	}

	for _, standing := range standings {
		if standing.Car.Driver.Name == "" {
			continue
		}

		if championship != nil && championship.DropWorstResults > 0 {
			// drops are applied before penalties, penalties are never dropped.
			standing.DroppedEvents, standing.DroppedPoints = dropCtx.dropWorstResults(championship.DropWorstResults, eventPoints[standing.Car.Driver.GUID])
			standing.Points -= standing.DroppedPoints
		}

		if !skipPointsPenalties {
			standing.Points -= float64(c.PenaltyForGUID(standing.Car.Driver.GUID))
		}

		out = append(out, standing)
	}

	numPositions := c.numFinishingPositions(events)

	sort.Slice(out, func(i, j int) bool {
		if out[i].Points == out[j].Points {
			// sort by number of wins (each race weekend session counts as an event)
			if len(events) > 1 {
				return c.HadBetterFinishingPositions(out[i].Car.Driver.GUID, out[j].Car.Driver.GUID, events, numPositions)
			}

			return out[i].Car.GetName() < out[j].Car.GetName()
		}

		return out[i].Points > out[j].Points
	})

	return out
}

// StandingsForEvent reports the standings for a single event, not including any generic points penalties applied to the championship.
func (c *ChampionshipClass) StandingsForEvent(championship *Championship, event *ChampionshipEvent) []*ChampionshipStanding {
	return c.Standings(championship, []*ChampionshipEvent{event}, StandingsNoPointsPenalties)
}

type ChampionshipStandingWithTableInfo struct {
	*ChampionshipStanding

	BackgroundColour template.CSS
	NoFinishReason   string
}

func (c *ChampionshipClass) StandingsForEntrantAtEvent(championship *Championship, event *ChampionshipEvent, entrant *ChampionshipStanding) *ChampionshipStandingWithTableInfo {

	outputStanding := &ChampionshipStanding{
		Car:    entrant.Car,
		Teams:  entrant.Teams,
		Points: 0.0,
	}

	backgroundColour := "#ffffff"
	noFinishReason := "DNS"

	for i, standing := range c.StandingsForEvent(championship, event) {
		if standing.Car.GetGUID() == entrant.Car.GetGUID() {

			switch {
			case standing.Points == 0.0:
				backgroundColour = "#ebcaff"
				noFinishReason = "0"
			case i == 0:
				backgroundColour = "#fefebe"
			case i == 1:
				backgroundColour = "#dce0e1"
			case i == 2:
				backgroundColour = "#ffde9e"
			case i > 2 && i <= 9:
				backgroundColour = fmt.Sprintf("rgb(%d, %d, %d)", 223-(i*3), 254-(i*3), 223-(i*3))
			case i > 9:
				backgroundColour = fmt.Sprintf("rgb(%d, %d, %d)", 208-(i*3), 206-(i*3), 255-(i*3))
			}

			outputStanding = standing
			break
		}
	}

	return &ChampionshipStandingWithTableInfo{
		ChampionshipStanding: outputStanding,
		BackgroundColour:     template.CSS(backgroundColour),
		NoFinishReason:       noFinishReason,
	}
}

// TeamStanding is the current number of Points a Team has.
type TeamStanding struct {
	Team   string
	Points float64

	// DroppedEvents/DroppedPoints: see ChampionshipStanding. Points already has the dropped results subtracted.
	DroppedEvents map[string]bool
	DroppedPoints float64
}

// IsDropped reports whether the result of the given Championship Event is dropped for this team.
func (ts *TeamStanding) IsDropped(eventID uuid.UUID) bool {
	return ts.DroppedEvents[eventID.String()]
}

// TeamStandings returns the current position of Teams in the Championship.
func (c *ChampionshipClass) TeamStandings(championship *Championship, inEvents []*ChampionshipEvent) []*TeamStanding {
	teams := make(map[string]float64)
	teamEventPoints := make(map[string]map[string]float64)
	dropCtx := newDropContext(inEvents)

	// make a copy of events so we do not persist race weekend sessions
	events := ExtractRaceWeekendSessionsIntoIndividualEvents(inEvents)

	c.standings(championship, events, func(event *ChampionshipEvent, driverGUID string, points float64, reason PointsReason) {
		var team string

		// find the team the driver was in for this race.
		for _, session := range event.Sessions {
			if session.Results != nil {
				for _, car := range session.Results.Cars {
					if car.Driver.GUID == driverGUID {
						team = car.Driver.Team
						break
					}

					if serverconfig.NormaliseEntrantGUIDs(car.Driver.GuidsList) == driverGUID {
						team = car.Driver.Team
						break
					}
				}
				break
			}
		}

		if _, ok := teams[team]; !ok {
			teams[team] = points
		} else {
			teams[team] += points
		}

		if teamEventPoints[team] == nil {
			teamEventPoints[team] = make(map[string]float64)
		}

		teamEventPoints[team][dropCtx.eventID(event.ID)] += points
	})

	var out []*TeamStanding

	for name, pts := range teams {
		standing := &TeamStanding{Team: name}

		if championship != nil && championship.DropWorstResults > 0 {
			// drops are applied before penalties, penalties are never dropped.
			standing.DroppedEvents, standing.DroppedPoints = dropCtx.dropWorstResults(championship.DropWorstResults, teamEventPoints[name])
			pts -= standing.DroppedPoints
		}

		standing.Points = pts - float64(c.PenaltyForTeam(name))

		out = append(out, standing)
	}

	// teams can have several cars per race, so all finishing positions need to be compared, not just len(teams).
	numPositions := c.numFinishingPositions(events)

	sort.Slice(out, func(i, j int) bool {
		if out[i].Points == out[j].Points {
			// sort by number of wins (each race weekend session counts as an event)
			if len(events) > 1 {
				return c.TeamHadBetterFinishingPositions(out[i].Team, out[j].Team, events, numPositions)
			}

			return out[i].Team < out[j].Team
		}

		return out[i].Points > out[j].Points
	})

	return out
}

// Ordinal returns the ordinal suffix of x (st, nd, rd or th).
func Ordinal(x int64) string {
	suffix := "th"
	switch x % 10 {
	case 1:
		if x%100 != 11 {
			suffix = "st"
		}
	case 2:
		if x%100 != 12 {
			suffix = "nd"
		}
	case 3:
		if x%100 != 13 {
			suffix = "rd"
		}
	}

	return suffix
}
