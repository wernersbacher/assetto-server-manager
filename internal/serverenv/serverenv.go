// Package serverenv carries the process wide state which the managers need but which is not owned by them: the id of
// this server manager instance, the installation path of the Assetto Corsa server and the panic handler. The values
// are functions because they are only known after the managers have been created (the install path is set after the
// resolver, the server id once the store has been initialised).
package serverenv

import (
	"path/filepath"

	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smlua "github.com/wernersbacher/assetto-server-manager/internal/lua"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

// Env is the environment of the managers. All fields have to be set.
type Env struct {
	// ServerID identifies this server manager instance.
	ServerID func() smmodel.ServerID

	// InstallPath is the directory in which the Assetto Corsa server is installed.
	InstallPath func() string

	// FallBackSorting returns true if loaded results are to use SessionResults.FallBackSort (a server option).
	FallBackSorting func() bool

	// PanicCapture runs fn and recovers from (and logs) a panic. It wraps background goroutines.
	PanicCapture func(fn func())

	// Debug is true if the server manager runs in debug mode.
	Debug bool
}

// TrackInfo returns the (cached) details of a track, or nil if the track is unknown.
func (e *Env) TrackInfo(track, layout string) *smcontent.TrackInfo {
	return smcontent.CachedTrackInfo(e.InstallPath(), track, layout)
}

// TrackSummary returns the name of a track including its layout, as shown to the users.
func (e *Env) TrackSummary(track, layout string) string {
	return smcontent.TrackSummary(e.InstallPath(), track, layout)
}

// TrackDownloadLink returns the URL from which a track can be downloaded, or an empty string if it is not known.
func (e *Env) TrackDownloadLink(track string) string {
	return smcontent.TrackDownloadLink(e.InstallPath(), track)
}

// Results returns the loader of the results files of the server. If Lua is enabled, the results Lua plugin is applied
// to every loaded result.
func (e *Env) Results() *smresults.Loader {
	loader := &smresults.Loader{
		Dir:             filepath.Join(e.InstallPath(), "results"),
		FallBackSorting: e.FallBackSorting(),
		PrettifyName:    func(name string) string { return smcontent.PrettifyName(name, false) },
	}

	if smlua.Initialised() {
		loader.OnLoad = resultsLoadPlugin
	}

	return loader
}

func resultsLoadPlugin(results *smresults.SessionResults) error {
	p := smlua.NewPlugin()

	newSessionResults := &smresults.SessionResults{}

	p.Inputs(results).Outputs(newSessionResults)
	err := p.Call("./plugins/results.lua", "onResultsLoad")

	if err != nil {
		return err
	}

	*results = *newSessionResults

	return nil
}
