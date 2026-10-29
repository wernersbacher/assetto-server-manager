package servermanager

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
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

var LaunchTime = time.Now()

type HealthCheck struct {
	raceControl *RaceControl
	process     ServerProcess
	store       smstore.Store
}

func NewHealthCheck(raceControl *RaceControl, store smstore.Store, process ServerProcess) *HealthCheck {
	return &HealthCheck{
		store:       store,
		raceControl: raceControl,
		process:     process,
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
		Version:       BuildVersion,
		NumCPU:        runtime.NumCPU(),
		NumGoroutines: runtime.NumGoroutine(),
		Uptime:        time.Since(LaunchTime).String(),
		GoVersion:     runtime.Version(),

		ServerName:          serverName,
		ServerID:            serverID,
		EventInProgress:     h.raceControl.process.IsRunning(),
		EventIsCritical:     !event.IsPractice() && (event.IsChampionship() || event.IsRaceWeekend() || h.raceControl.SessionInfo.Type == udp.SessionTypeRace || h.raceControl.SessionInfo.Type == udp.SessionTypeQualifying),
		EventIsChampionship: event.IsChampionship(),
		EventIsRaceWeekend:  event.IsRaceWeekend(),
		EventIsPractice:     event.IsPractice(),
		NumConnectedDrivers: h.raceControl.ConnectedDrivers.Len(),
		AssettoIsInstalled:  IsAssettoInstalled(),
		StrackerIsInstalled: smplugins.IsStrackerInstalled(ServerInstallPath),

		ConfigDirectoryIsWritable:  IsDirWriteable(filepath.Join(ServerInstallPath, "cfg")) == nil,
		CarDirectoryIsWritable:     IsDirWriteable(filepath.Join(ServerInstallPath, "content", "cars")) == nil,
		TrackDirectoryIsWritable:   IsDirWriteable(filepath.Join(ServerInstallPath, "content", "tracks")) == nil,
		WeatherDirectoryIsWritable: IsDirWriteable(filepath.Join(ServerInstallPath, "content", "weather")) == nil,
		SetupsDirectoryIsWritable:  IsDirWriteable(filepath.Join(ServerInstallPath, "setups")) == nil,
		ResultsDirectoryIsWritable: IsDirWriteable(filepath.Join(ServerInstallPath, "results")) == nil,
	})
}

func IsDirWriteable(dir string) error {
	file := filepath.Join(dir, ".test-write")

	if err := ioutil.WriteFile(file, []byte(""), 0600); err != nil {
		return err
	}

	return os.Remove(file)
}
