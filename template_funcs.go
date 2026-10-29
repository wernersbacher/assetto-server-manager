package servermanager

import (
	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	"github.com/wernersbacher/assetto-server-manager/internal/contenthttp"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smraceweekend "github.com/wernersbacher/assetto-server-manager/internal/raceweekend"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"

	"html/template"
	"strings"
)

// BuildVersion is the time Server Manager was built at
var BuildVersion string

func driverInitials(name string) string {
	return smresults.DriverInitials(name)
}

var Changelog template.HTML

func changelogHTML() template.HTML {
	return Changelog
}

func varSplit(str string) []string {
	return strings.Split(str, ";")
}

func prettifyName(s string, acronyms bool) string {
	return smcontent.PrettifyName(s, acronyms)
}

func weatherName(installPath, key string) string {
	weathers, err := smcontent.ListWeather(installPath)

	if err != nil {
		return key
	}

	key = strings.Split(key, "_type=")[0]

	if name, ok := weathers[key]; ok {
		return name
	}

	return prettifyName(key, false)
}

// serverTemplateFuncs returns the template functions which depend on the server manager's own state and domain code.
// They are added to the functions which the web renderer provides itself.
func serverTemplateFuncs(env *serverenv.Env) template.FuncMap {
	return template.FuncMap{
		"weatherName":              func(key string) string { return weatherName(env.InstallPath(), key) },
		"carList":                  smcontent.CarList,
		"varSplit":                 varSplit,
		"trackInfo":                env.TrackInfo,
		"classColor":               smchampionship.ChampionshipClassColor,
		"carSkinURL":               func(car, skin string) string { return contenthttp.CarSkinURL(env.InstallPath(), car, skin) },
		"trackLayoutURL":           func(track, layout string) string { return smcontent.TrackLayoutURL(env.InstallPath(), track, layout) },
		"AccountGroup":             func(a smmodel.Account) smmodel.Group { return a.GroupFor(env.ServerID()) },
		"raceWeekendTrackOverview": func(rw *smmodel.RaceWeekend) string { return smraceweekend.TrackOverview(env, rw) },
		"EventName":                func(e smmodel.RaceEvent) string { return e.EventName(env.TrackSummary) },
		"EventIsLooping":           func(e smmodel.RaceEvent) bool { return e.IsLooping(env.ServerID()) },
		"driverInitials":           driverInitials,
		"ChangelogHTML":            changelogHTML,
	}
}
