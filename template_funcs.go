package servermanager

import (
	smchampionship "github.com/wernersbacher/ACPitwall/internal/championship"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	"github.com/wernersbacher/ACPitwall/internal/contenthttp"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smraceweekend "github.com/wernersbacher/ACPitwall/internal/raceweekend"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"

	"html/template"
	"strings"
)

// BuildVersion is the time ACPitwall was built at
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

// serverTemplateFuncs returns the template functions which depend on ACPitwall's own state and domain code.
// They are added to the functions which the web renderer provides itself.
func serverTemplateFuncs(env *serverenv.Env, servers smweb.ServersFunc) template.FuncMap {
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
		"resultServer":             func(fileName string) *smweb.ServerRef { return resultServer(env, servers, fileName) },
		"driverInitials":           driverInitials,
		"ChangelogHTML":            changelogHTML,
	}
}

// resultServer returns the server whose results directory holds the results file, so that penalty and blacklist forms
// act on the server the results come from. It is nil (the forms use the current server) if there are fewer than two
// servers or the file is not found in a server's directory.
func resultServer(env *serverenv.Env, servers smweb.ServersFunc, fileName string) *smweb.ServerRef {
	attributed := servers.Attributed()

	if len(attributed) == 0 || fileName == "" {
		return nil
	}

	return smweb.ServerOfDir(attributed, env.Results().DirOf(fileName))
}
