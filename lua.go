package servermanager

import (
	smlua "github.com/wernersbacher/assetto-server-manager/internal/lua"
	racecontrol "github.com/wernersbacher/assetto-server-manager/internal/racecontrol"

	glua "github.com/yuin/gopher-lua"
)

// InitLua enables the Lua plugins, which can also talk to the race control.
func InitLua(raceControl *racecontrol.RaceControl) {
	smlua.Init(map[string]glua.LGFunction{
		"broadcastChat": raceControl.LuaBroadcastChat,
		"sendChat":      raceControl.LuaSendChat,
	})
}
