package racecontrol

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smplugins "github.com/wernersbacher/ACPitwall/internal/plugins"
	smserver "github.com/wernersbacher/ACPitwall/internal/server"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second
)

type Broadcaster interface {
	Send(message udp.Message) ([]byte, error)
}

type NilBroadcaster struct{}

func (NilBroadcaster) Send(message udp.Message) ([]byte, error) {
	logrus.WithField("message", message).Debugf("Message send %d", message.Event())
	return nil, nil
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func encodeRaceControlMessage(message udp.Message) ([]byte, error) {
	m := raceControlMessage{
		EventType: message.Event(),
		Message:   message,
	}

	return json.Marshal(m)
}

type raceControlMessage struct {
	EventType udp.Event
	Message   udp.Message
}

type RaceControlHub struct {
	clients   map[*raceControlClient]bool
	broadcast chan []byte
	register  chan *raceControlClient
}

func (h *RaceControlHub) Send(message udp.Message) ([]byte, error) {
	encoded, err := encodeRaceControlMessage(message)

	if err != nil {
		return nil, err
	}

	h.broadcast <- encoded

	return encoded, nil
}

// NewRaceControlHub creates the hub which sends the live timing updates to the connected browsers. Run has to be
// started in a goroutine.
func NewRaceControlHub() *RaceControlHub {
	return &RaceControlHub{
		broadcast: make(chan []byte, 1000),
		register:  make(chan *raceControlClient),
		clients:   make(map[*raceControlClient]bool),
	}
}

// Run distributes the messages to the clients, it blocks forever.
func (h *RaceControlHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.receive <- message:
				default:
					close(client.receive)
					delete(h.clients, client)
				}
			}
		}
	}
}

type raceControlClient struct {
	hub *RaceControlHub

	conn    *websocket.Conn
	receive chan []byte
}

func (c *raceControlClient) writePump() {
	ticker := time.NewTicker(time.Second * 10)
	defer func() {
		if rvr := recover(); rvr != nil {
			logrus.WithField("panic", rvr).Errorf("Recovered from panic")
		}
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.receive:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			err := c.conn.WriteMessage(websocket.TextMessage, message)

			if err != nil && !strings.HasSuffix(err.Error(), "write: broken pipe") {
				logrus.WithError(err).Errorf("Could not send websocket message")
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// CurrentRaceProvider returns the race which is running on the server, and its entry list.
type CurrentRaceProvider interface {
	CurrentRace() (*serverconfig.ServerConfig, serverconfig.EntryList)
}

// RaceControlHandler serves the live timing page and the race control actions.
type RaceControlHandler struct {
	*smweb.BaseHandler

	slotFor SlotFor
}

// NewRaceControlHandler creates the handler of the live timing.
func NewRaceControlHandler(baseHandler *smweb.BaseHandler, slotFor SlotFor) *RaceControlHandler {
	return &RaceControlHandler{
		BaseHandler: baseHandler,
		slotFor:     slotFor,
	}
}

type liveTimingTemplateVars struct {
	smweb.BaseTemplateVars

	RaceDetails                 *smmodel.CustomRace
	FrameLinks                  []string
	CSSDotSmoothing             int
	CMJoinLink                  string
	UseMPH                      bool
	IsStrackerEnabled           bool
	IsKissMyRankEnabled         bool
	KissMyRankWebStatsPublicURL string
	STrackerInterfacePublicURL  string
}

func (rch *RaceControlHandler) LiveTiming(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	currentRace, entryList := slot.RaceManager.CurrentRace()

	var customRace *smmodel.CustomRace

	if currentRace != nil {
		customRace = &smmodel.CustomRace{EntryList: entryList, RaceConfig: currentRace.CurrentRaceConfig}
	}

	frameLinks, err := slot.Store.ListPrevFrames()

	if err != nil {
		logrus.WithError(err).Errorf("could not get frame links")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	serverOpts, err := slot.Store.LoadServerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load server options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	linkString := ""

	if serverOpts.ShowContentManagerJoinLink == 1 {
		link, err := smserver.GetContentManagerJoinLink(*serverOpts)

		if err != nil {
			logrus.WithError(err).Errorf("could not get content manager join link")
		} else {
			linkString = link.String()
		}
	}

	strackerOptions, err := slot.Store.LoadStrackerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load stracker options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sTrackerPublicURL := strackerOptions.HTTPConfiguration.PublicURL

	if sTrackerPublicURL == "" {
		sTrackerPublicURL = "/stracker/mainpage"
	}

	kissMyRankOptions, err := slot.Store.LoadKissMyRankOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load kissmyrank options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rch.ViewRenderer.MustLoadTemplate(w, r, "live-timing.html", &liveTimingTemplateVars{
		BaseTemplateVars: smweb.BaseTemplateVars{
			WideContainer: true,
		},
		RaceDetails:                 customRace,
		FrameLinks:                  frameLinks,
		CSSDotSmoothing:             udp.RealtimePosIntervalMs,
		CMJoinLink:                  linkString,
		UseMPH:                      serverOpts.UseMPH == 1,
		IsStrackerEnabled:           smplugins.IsStrackerInstalled(slot.Env.InstallPath()) && strackerOptions.EnableStracker,
		IsKissMyRankEnabled:         smplugins.IsKissMyRankInstalled(slot.Env.InstallPath()) && kissMyRankOptions.EnableKissMyRank,
		KissMyRankWebStatsPublicURL: kissMyRankOptions.WebStatsPublicURL,
		STrackerInterfacePublicURL:  sTrackerPublicURL,
	})
}

func deleteEmpty(s []string) []string {
	var r []string
	for _, str := range s {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}

func (rch *RaceControlHandler) SaveIFrames(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	// Save the frame links from the form
	err := r.ParseForm()

	if err != nil {
		logrus.WithError(err).Errorf("could not load parse form")
		return
	}

	err = slot.Store.UpsertLiveFrames(deleteEmpty(r.Form["frame-link"]))

	if err != nil {
		logrus.WithError(err).Errorf("could not save frame links")
		return
	}

	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (rch *RaceControlHandler) Websocket(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	c, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		logrus.Error(err)
		return
	}

	client := &raceControlClient{hub: slot.Hub, conn: c, receive: make(chan []byte, 256)}
	client.hub.register <- client

	go client.writePump()

	// new client, send them an initial race control message.
	slot.RaceControl.lastUpdateMessageMutex.Lock()
	client.receive <- slot.RaceControl.lastUpdateMessage
	slot.RaceControl.lastUpdateMessageMutex.Unlock()

	// send stored chat messages to new client
	slot.RaceControl.ChatMessagesMutex.Lock()

	for _, message := range slot.RaceControl.ChatMessages {
		encoded, err := encodeRaceControlMessage(message)

		if err != nil {
			continue
		}

		client.receive <- encoded
	}

	slot.RaceControl.ChatMessagesMutex.Unlock()
}

func (rch *RaceControlHandler) BroadcastChat(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		return
	}

	err := slot.RaceControl.splitAndBroadcastChat(r.FormValue("broadcast-chat"), smweb.AccountFromRequest(r))

	if err != nil {
		logrus.WithError(err).Errorf("Unable to broadcast chat message")
	}
}

func (rch *RaceControlHandler) AdminCommand(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		return
	}

	adminCommand, err := udp.NewAdminCommand(r.FormValue("admin-command"))

	if err == nil {
		err := slot.Process.SendUDPMessage(adminCommand)

		if err != nil {
			logrus.WithError(err).Errorf("Unable to send admin command")
		}
	} else {
		logrus.WithError(err).Errorf("Unable to build admin command")
	}
}

func (rch *RaceControlHandler) KickUser(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		return
	}

	guid := r.FormValue("kick-user")

	if (guid == "") || (guid == "default-driver-spacer") {
		return
	}

	err := slot.RaceControl.ConnectedDrivers.Each(func(driverGUID udp.DriverGUID, driver *smmodel.RaceControlDriver) error {
		if string(driverGUID) != guid {
			return nil
		}

		command, err := udp.NewAdminCommand("/kick " + driver.CarInfo.DriverName)

		if err != nil {
			return err
		}

		return slot.Process.SendUDPMessage(command)
	})

	if err != nil {
		logrus.WithError(err).Errorf("Unable to send kick command")
	}
}

func (rch *RaceControlHandler) SendChat(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		return
	}

	guid := r.FormValue("chat-user")

	if (guid == "") || (guid == "default-driver-spacer") {
		return
	}

	err := slot.RaceControl.splitAndSendChat(r.FormValue("send-chat"), guid)

	if err != nil {
		logrus.WithError(err).Errorf("Unable to send chat message to driver: %s", guid)
	}
}

func (rch *RaceControlHandler) RestartSession(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	err := slot.Process.SendUDPMessage(&udp.RestartSession{})

	if err != nil {
		logrus.WithError(err).Errorf("Unable to restart session")

		smweb.AddErrorFlash(w, r, "The server was unable to restart the session!")
	}

	http.Redirect(w, r, smweb.ServerURL(r, "/live-timing"), http.StatusFound)
}

func (rch *RaceControlHandler) NextSession(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	err := slot.Process.SendUDPMessage(&udp.NextSession{})

	if err != nil {
		logrus.WithError(err).Errorf("Unable to move to next session")

		smweb.AddErrorFlash(w, r, "The server was unable to move to the next session!")
	}

	http.Redirect(w, r, smweb.ServerURL(r, "/live-timing"), http.StatusFound)
}

func (rch *RaceControlHandler) Countdown(w http.ResponseWriter, r *http.Request) {
	slot, ok := rch.slot(w, r)

	if !ok {
		return
	}

	// broadcast countdown
	ticker := time.NewTicker(time.Second * 3)
	i := 4

	for range ticker.C {
		var countdown string

		i--

		if i > 0 {
			countdown = strconv.Itoa(i)
		} else if i == 0 {
			countdown = "GO"
		} else {
			ticker.Stop()

			return
		}

		err := slot.RaceControl.splitAndBroadcastChat(countdown, nil)

		if err != nil {
			logrus.WithError(err).Error("Unable to broadcast countdown message")
		}
	}
}
