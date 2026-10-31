package admin

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smplugins "github.com/wernersbacher/ACPitwall/internal/plugins"
	"github.com/wernersbacher/ACPitwall/internal/steam"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

var LaunchTime = time.Now()

// HealthCheck serves the health of the server manager as JSON.
type HealthCheck struct {
	slotFor      SlotFor
	buildVersion string
}

func NewHealthCheck(slotFor SlotFor, buildVersion string) *HealthCheck {
	return &HealthCheck{
		slotFor:      slotFor,
		buildVersion: buildVersion,
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
	slot, err := h.slotFor(r)

	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	event := slot.Process.Event()
	opts, err := slot.Store.LoadServerOptions()

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
		ServerID:            slot.Env.ServerID(),
		EventInProgress:     slot.Process.IsRunning(),
		EventIsCritical:     !event.IsPractice() && (event.IsChampionship() || event.IsRaceWeekend() || slot.RaceControl.SessionInfo.Type == udp.SessionTypeRace || slot.RaceControl.SessionInfo.Type == udp.SessionTypeQualifying),
		EventIsChampionship: event.IsChampionship(),
		EventIsRaceWeekend:  event.IsRaceWeekend(),
		EventIsPractice:     event.IsPractice(),
		NumConnectedDrivers: slot.RaceControl.ConnectedDrivers.Len(),
		AssettoIsInstalled:  steam.IsAssettoInstalled(slot.Env.InstallPath()),
		StrackerIsInstalled: smplugins.IsStrackerInstalled(slot.Env.InstallPath()),

		ConfigDirectoryIsWritable:  IsDirWriteable(filepath.Join(slot.Env.InstallPath(), "cfg")) == nil,
		CarDirectoryIsWritable:     IsDirWriteable(filepath.Join(slot.Env.InstallPath(), "content", "cars")) == nil,
		TrackDirectoryIsWritable:   IsDirWriteable(filepath.Join(slot.Env.InstallPath(), "content", "tracks")) == nil,
		WeatherDirectoryIsWritable: IsDirWriteable(filepath.Join(slot.Env.InstallPath(), "content", "weather")) == nil,
		SetupsDirectoryIsWritable:  IsDirWriteable(filepath.Join(slot.Env.InstallPath(), "setups")) == nil,
		ResultsDirectoryIsWritable: IsDirWriteable(filepath.Join(slot.Env.InstallPath(), "results")) == nil,
	})
}

func IsDirWriteable(dir string) error {
	file := filepath.Join(dir, ".test-write")

	if err := ioutil.WriteFile(file, []byte(""), 0600); err != nil {
		return err
	}

	return os.Remove(file)
}
