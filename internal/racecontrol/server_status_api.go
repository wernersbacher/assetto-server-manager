package racecontrol

import (
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	"github.com/wernersbacher/assetto-server-manager/internal/api"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

// The server status API exposes what the live timing page shows about the running event (track, session and the
// connected drivers) in a stable JSON format, so that other systems like websites or bots can read it. Passwords,
// driver GUIDs, IP addresses, lap times and the server configuration are intentionally left out. Driver names are
// shortened if the server is set to hide last names.

type APIServerStatus struct {
	// Running is false if no event is running. All other fields except ServerName are then empty.
	Running    bool
	ServerName string `json:",omitempty"`

	EventName           string `json:",omitempty"`
	IsChampionship      bool
	ChampionshipName    string `json:",omitempty"`
	IsRaceWeekend       bool
	RaceWeekendName     string `json:",omitempty"`
	IsPractice          bool
	IsTimeAttack        bool
	Track               string `json:",omitempty"`
	TrackLayout         string `json:",omitempty"`
	TrackName           string `json:",omitempty"`
	MaxClients          int
	NumConnectedDrivers int

	// Session is the session that is currently running, if the server has reported it yet.
	Session *APIServerSession `json:",omitempty"`

	ConnectedDrivers []*APIConnectedDriver
}

type APIServerSession struct {
	Type serverconfig.SessionType
	Name string

	// Time is the length of the session in minutes, Laps the number of laps. Only one of them is set.
	Time int `json:",omitempty"`
	Laps int `json:",omitempty"`

	Started time.Time
}

type APIConnectedDriver struct {
	Name     string
	CarModel string
	CarName  string
	CarSkin  string
}

// NewAPIServerStatus converts the state of the server process and race control to the API format. serverName is the
// configured server name, used when the running server has not reported its own yet.
func NewAPIServerStatus(env *serverenv.Env, running bool, serverName string, event smmodel.RaceEvent, raceControl *RaceControl) *APIServerStatus {
	out := &APIServerStatus{
		Running:          running,
		ServerName:       serverName,
		ConnectedDrivers: []*APIConnectedDriver{},
	}

	if !running {
		return out
	}

	if raceControl.SessionInfo.ServerName != "" {
		out.ServerName = raceControl.SessionInfo.ServerName
	}

	out.EventName = event.EventName(env.TrackSummary)
	out.IsChampionship = event.IsChampionship()
	out.IsRaceWeekend = event.IsRaceWeekend()
	out.IsPractice = event.IsPractice()
	out.IsTimeAttack = event.IsTimeAttack()

	switch e := event.(type) {
	case *smmodel.ActiveChampionship:
		out.ChampionshipName = e.Name
	case smmodel.ActiveRaceWeekend:
		out.RaceWeekendName = e.Name
	case *smmodel.ActiveRaceWeekend:
		out.RaceWeekendName = e.Name
	}

	raceConfig := event.GetRaceConfig()

	out.MaxClients = raceConfig.MaxClients

	// the server reports the track it is actually running, fall back to the event's track until it has done so
	out.Track, out.TrackLayout = raceConfig.Track, raceConfig.TrackLayout

	sessionInfo := raceControl.SessionInfo

	if sessionInfo.Track != "" {
		out.Track, out.TrackLayout = sessionInfo.Track, sessionInfo.TrackConfig
	}

	if out.Track != "" {
		out.TrackName = api.TrackName(env.InstallPath(), out.Track, out.TrackLayout)
	}

	if !raceControl.SessionStartTime.IsZero() {
		out.Session = &APIServerSession{
			Type:    apiSessionType(sessionInfo.Type),
			Name:    sessionInfo.Name,
			Started: raceControl.SessionStartTime,
		}

		if sessionInfo.Time > 0 {
			out.Session.Time = int(sessionInfo.Time)
		} else {
			out.Session.Laps = int(sessionInfo.Laps)
		}
	}

	_ = raceControl.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
		driver.Lock()
		carInfo := driver.CarInfo
		driver.Unlock()

		out.ConnectedDrivers = append(out.ConnectedDrivers, &APIConnectedDriver{
			Name:     smresults.DriverName(carInfo.DriverName),
			CarModel: carInfo.CarModel,
			CarName:  smcontent.PrettifyName(carInfo.CarModel, true),
			CarSkin:  carInfo.CarSkin,
		})

		return nil
	})

	out.NumConnectedDrivers = len(out.ConnectedDrivers)

	return out
}

// serverStatusAPI returns the current state of the server in the API format.
func (rch *RaceControlHandler) ServerStatusAPI(w http.ResponseWriter, r *http.Request) {
	var serverName string

	opts, err := rch.store.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load server options")
	} else {
		serverName = opts.Name
	}

	api.WriteJSON(w, NewAPIServerStatus(rch.env, rch.serverProcess.IsRunning(), serverName, rch.serverProcess.Event(), rch.raceControl))
}

// apiSessionType converts the session type of the UDP plugin protocol to the session types used in results and
// championships.
func apiSessionType(sessionType udp.SessionType) serverconfig.SessionType {
	switch sessionType {
	case udp.SessionTypeBooking:
		return serverconfig.SessionTypeBooking
	case udp.SessionTypePractice:
		return serverconfig.SessionTypePractice
	case udp.SessionTypeQualifying:
		return serverconfig.SessionTypeQualifying
	case udp.SessionTypeRace:
		return serverconfig.SessionTypeRace
	default:
		return serverconfig.SessionType(sessionType.String())
	}
}
