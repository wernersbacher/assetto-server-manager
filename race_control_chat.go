package servermanager

import (
	"strings"

	"github.com/mitchellh/go-wordwrap"
	"github.com/sirupsen/logrus"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
	lua "github.com/yuin/gopher-lua"
)

const (
	chatMessageLimit  = 50
	chatCommandPrefix = "/"
)

func (rc *RaceControl) OnChatMessage(chat udp.Chat) error {
	if strings.HasPrefix(chat.Message, chatCommandPrefix) {
		return nil
	}

	_, err := rc.broadcaster.Send(chat)

	if err != nil {
		return err
	}

	rc.ChatMessagesMutex.Lock()

	rc.ChatMessages = append(rc.ChatMessages, chat)

	if len(rc.ChatMessages) > chatMessageLimit {
		rc.ChatMessages = rc.ChatMessages[len(rc.ChatMessages)-chatMessageLimit:]
	}

	rc.ChatMessagesMutex.Unlock()

	if rc.config.Lua.Enabled {
		go func() {
			err := chatMessagePlugin(chat)

			if err != nil {
				logrus.WithError(err).Error("chat message plugin script failed")
			}
		}()
	}

	return nil
}

func chatMessagePlugin(chat udp.Chat) error {
	p := NewLuaPlugin()

	p.Inputs(chat)

	err := p.Call("./plugins/race-control.lua", "onChat")

	if err != nil {
		return err
	}

	return nil
}

func (rc *RaceControl) LuaBroadcastChat(L *lua.LState) int {
	message := L.ToString(1)

	err := rc.splitAndBroadcastChat(message, nil)

	if err != nil {
		logrus.WithError(err).Errorf("Unable to broadcast chat message")
		L.Push(lua.LBool(false))
	} else {
		L.Push(lua.LBool(true))
	}

	return 1
}

func (rc *RaceControl) LuaSendChat(L *lua.LState) int {
	message := L.ToString(1)
	guid := L.ToString(2)

	err := rc.splitAndSendChat(message, guid)

	if err != nil {
		logrus.WithError(err).Errorf("Unable to broadcast chat message")
		L.Push(lua.LBool(false))
	} else {
		L.Push(lua.LBool(true))
	}

	return 1
}

func (rc *RaceControl) splitAndBroadcastChat(message string, account *smmodel.Account) error {
	name := "Server"
	guid := ""

	if account != nil {
		name = account.Name
		guid = account.GUID
	}

	messageWithUser := "(" + name + ") " + message

	wrapped := strings.Split(wordwrap.WrapString(
		messageWithUser,
		60,
	), "\n")

	for _, msg := range wrapped {
		broadcastMessage, err := udp.NewBroadcastChat(msg)

		if err == nil {
			err := rc.process.SendUDPMessage(broadcastMessage)

			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	chat, err := udp.NewChat(message, 0, name, udp.DriverGUID(guid))

	if err == nil {
		return rc.OnChatMessage(chat)
	}

	logrus.WithError(err).Error("Chat broadcasted successfully but could not be added to the live timings chat window!")

	return nil
}

func (rc *RaceControl) splitAndSendChat(message, guid string) error {
	var carID uint8

	for id, rangeGUID := range rc.CarIDToGUID {
		if string(rangeGUID) == guid {
			carID = uint8(id)
			break
		}
	}

	wrapped := strings.Split(wordwrap.WrapString(
		message,
		60,
	), "\n")

	for _, msg := range wrapped {
		welcomeMessage, err := udp.NewSendChat(udp.CarID(carID), msg)

		if err == nil {
			err := rc.process.SendUDPMessage(welcomeMessage)

			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}
