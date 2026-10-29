package servermanager

type ChampionshipTab string

const (
	ChampionshipTabDriverStandings ChampionshipTab = "Driver Standings"
	ChampionshipTabTeamStandings   ChampionshipTab = "Team Standings"
	ChampionshipTabOverview        ChampionshipTab = "Overview"
	ChampionshipTabEntrants        ChampionshipTab = "Entrants"
	ChampionshipTabPointsReference ChampionshipTab = "Points Reference"
)

var AvailableChampionshipTabs = []ChampionshipTab{
	ChampionshipTabDriverStandings,
	ChampionshipTabTeamStandings,
	ChampionshipTabOverview,
	ChampionshipTabEntrants,
	ChampionshipTabPointsReference,
}
