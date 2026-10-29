package accounts

import smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"

type themeDetails struct {
	Theme smmodel.Theme
	Name  string
}

var themeOptions = []themeDetails{
	{
		Theme: smmodel.ThemeDefault,
		Name:  "Use Default",
	},
	{
		Theme: smmodel.ThemeLight,
		Name:  "Light",
	},
	{
		Theme: smmodel.ThemeDark,
		Name:  "Dark",
	},
}
