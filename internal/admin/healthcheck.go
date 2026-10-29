package admin

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
	racecontrol "github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	smserver "github.com/wernersbacher/assetto-server-manager/internal/server"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	"github.com/wernersbacher/assetto-server-manager/internal/steam"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

var LaunchTime = time.Now()

// HealthCheck serves the health of the server manager as JSON.
type HealthCheck struct {
	raceControl  *racecontrol.RaceControl
	process      smserver.ServerProcess
	store        smstore.Store
	env          *serverenv.Env
	buildVersion string
}

func NewHealthCheck(raceControl *racecontrol.RaceControl, store smstore.Store, process smserver.ServerProcess, env *serverenv.Env, buildVersion string) *HealthCheck {
	return &HealthCheck{
		env:          env,
		buildVersion: buildVersion,
		store:        store,
		raceControl:  raceControl,
		process:      process,
	}
}

type HealthCheckResponse struct {
	OK      bool
	Version string

	OS            string
	NumCPU        int
	NumGoroutines int
	Uptime        string
	GoVersion     string

	AssettoIsInstalled  bool
	StrackerIsInstalled bool

	CarDirectoryIsWritable     bool
	TrackDirectoryIsWritable   bool
	WeatherDirectoryIsWritable bool
	SetupsDirectoryIsWritable  bool
	ConfigDirectoryIsWritable  bool
	ResultsDirectoryIsWritable bool

	ServerName          string
	ServerID            smmodel.ServerID
	EventInProgress     bool
	EventIsCritical     bool
	EventIsChampionship bool
	EventIsRaceWeekend  bool
	EventIsPractice     bool
	NumConnectedDrivers int
}

func (h *HealthCheck) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	event := h.process.Event()
	opts, err := h.store.LoadServerOptions()

	var serverName string

	if err == nil {
		serverName = opts.Name
	}

	_ = json.NewEncoder(w).Encode(HealthCheckResponse{
		OK:            true,
		OS:            runtime.GOOS + "/" + runtime.GOARCH,
		Version:       h.buildVersion,
		NumCPU:        runtime.NumCPU(),
		NumGoroutines: runtime.NumGoroutine(),
		Uptime:        time.Since(LaunchTime).String(),
		GoVersion:     runtime.Version(),

		ServerName:          serverName,
		ServerID:            h.env.ServerID(),
		EventInProgress:     h.process.IsRunning(),
		EventIsCritical:     !event.IsPractice() && (event.IsChampionship() || event.IsRaceWeekend() || h.raceControl.SessionInfo.Type == udp.SessionTypeRace || h.raceControl.SessionInfo.Type == udp.SessionTypeQualifying),
		EventIsChampionship: event.IsChampionship(),
		EventIsRaceWeekend:  event.IsRaceWeekend(),
		EventIsPractice:     event.IsPractice(),
		NumConnectedDrivers: h.raceControl.ConnectedDrivers.Len(),
		AssettoIsInstalled:  steam.IsAssettoInstalled(h.env.InstallPath()),
		StrackerIsInstalled: smplugins.IsStrackerInstalled(h.env.InstallPath()),

		ConfigDirectoryIsWritable:  IsDirWriteable(filepath.Join(h.env.InstallPath(), "cfg")) == nil,
		CarDirectoryIsWritable:     IsDirWriteable(filepath.Join(h.env.InstallPath(), "content", "cars")) == nil,
		TrackDirectoryIsWritable:   IsDirWriteable(filepath.Join(h.env.InstallPath(), "content", "tracks")) == nil,
		WeatherDirectoryIsWritable: IsDirWriteable(filepath.Join(h.env.InstallPath(), "content", "weather")) == nil,
		SetupsDirectoryIsWritable:  IsDirWriteable(filepath.Join(h.env.InstallPath(), "setups")) == nil,
		ResultsDirectoryIsWritable: IsDirWriteable(filepath.Join(h.env.InstallPath(), "results")) == nil,
	})
}

func IsDirWriteable(dir string) error {
	file := filepath.Join(dir, ".test-write")

	if err := ioutil.WriteFile(file, []byte(""), 0600); err != nil {
		return err
	}

	return os.Remove(file)
}
