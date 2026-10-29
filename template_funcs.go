package servermanager

import (
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"

	"html/template"
	"strings"
)

// BuildVersion is the time Server Manager was built at
var BuildVersion string

func driverName(name string) string {
	return smresults.DriverName(name)
}

func driverInitials(name string) string {

	name = strings.TrimSpace(name)

	if smresults.UseShortenedDriverNames {
		nameParts := strings.Split(name, " ")

		if len(nameParts) == 1 {
			return name
		}

		for i := range nameParts {
			if len(nameParts[i]) > 0 {
				nameParts[i] = nameParts[i][:1]
			}
		}

		return strings.ToUpper(strings.Join(nameParts, ""))
	}

	nameParts := strings.Split(name, " ")

	if len(nameParts) > 0 && len(nameParts[len(nameParts)-1]) >= 3 {
		return strings.ToUpper(nameParts[len(nameParts)-1][:3])
	}

	return strings.ToUpper(name)
}

var Changelog template.HTML

func changelogHTML() template.HTML {
	return Changelog
}

func varSplit(str string) []string {
	return strings.Split(str, ";")
}

func trackInfo(track, layout string) *smcontent.TrackInfo {
	return smcontent.CachedTrackInfo(ServerInstallPath, track, layout)
}

func clearFromTrackInfoCache(track, layout string) {
	smcontent.ClearTrackInfoCache(track, layout)
}

func prettifyName(s string, acronyms bool) string {
	return smcontent.PrettifyName(s, acronyms)
}

func weatherName(key string) string {
	weathers, err := smcontent.ListWeather(ServerInstallPath)

	if err != nil {
		return key
	}

	key = strings.Split(key, "_type=")[0]

	if name, ok := weathers[key]; ok {
		return name
	}

	return prettifyName(key, false)
}

func trackLayoutURL(track, layout string) string {
	return smcontent.TrackLayoutURL(ServerInstallPath, track, layout)
}

// serverTemplateFuncs returns the template functions which depend on the server manager's own state and domain code.
// They are added to the functions which the web renderer provides itself.
func serverTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"weatherName":              weatherName,
		"carList":                  smcontent.CarList,
		"varSplit":                 varSplit,
		"trackInfo":                trackInfo,
		"classColor":               ChampionshipClassColor,
		"carSkinURL":               carSkinURL,
		"trackLayoutURL":           trackLayoutURL,
		"AccountGroup":             func(a smmodel.Account) smmodel.Group { return a.GroupFor(serverID) },
		"raceWeekendTrackOverview": raceWeekendTrackOverview,
		"EventName":                func(e smmodel.RaceEvent) string { return e.EventName(trackSummary) },
		"EventIsLooping":           func(e smmodel.RaceEvent) bool { return e.IsLooping(serverID) },
		"driverInitials":           driverInitials,
		"ChangelogHTML":            changelogHTML,
	}
}
