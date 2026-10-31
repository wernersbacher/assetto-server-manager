// Package lua runs the Lua plugin scripts of ACPitwall ("./plugins/*.lua"). Lua is optional: the plugins are only
// active after Init has been called, which happens when Lua is enabled in the configuration.
package lua

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	lua "github.com/yuin/gopher-lua"
)

var functions = make(map[string]lua.LGFunction)

// initialised is set by Init, which is only called when Lua is enabled in the configuration.
var initialised bool

// Initialised returns true if Lua is enabled, i.e. Init has been called.
func Initialised() bool {
	return initialised
}

// Init enables the Lua plugins. The extra functions are made available to the scripts next to the built in
// httpRequest, and the manager start script is run.
func Init(extraFunctions map[string]lua.LGFunction) {
	initialised = true
	functions["httpRequest"] = HTTPRequest

	for name, fn := range extraFunctions {
		functions[name] = fn
	}

	go func() {
		err := managerStartPlugin()

		if err != nil {
			logrus.WithError(err).Error("manager start plugin script failed")
		}
	}()
}

// Plugin calls one function of a Lua script with JSON encoded inputs and outputs.
type Plugin struct {
	state *lua.LState

	inputs  []interface{}
	outputs []interface{}
}

// NewPlugin creates a plugin which can call the scripts in the plugins directory.
func NewPlugin() *Plugin {
	state := lua.NewState()

	for name, fn := range functions {
		state.SetGlobal(name, state.NewFunction(fn))
	}

	return &Plugin{
		state: state,
	}
}

func (l *Plugin) Inputs(i ...interface{}) *Plugin {
	l.inputs = append(l.inputs, i...)

	return l
}

// remember outputs need to be reversed from whatever the plugin returns
func (l *Plugin) Outputs(o ...interface{}) *Plugin {
	l.outputs = append(l.outputs, o...)

	return l
}

func (l *Plugin) Call(fileName, functionName string) error {
	defer l.state.Close()

	var jsonInputs []lua.LValue

	err := l.state.DoFile(fileName)

	if err != nil {
		return err
	}

	for _, input := range l.inputs {
		jsonInput, err := json.Marshal(input)

		if err != nil {
			return err
		}

		jsonInputs = append(jsonInputs, lua.LString(jsonInput))
	}

	if err := l.state.CallByParam(lua.P{
		Fn:      l.state.GetGlobal(functionName), // name of Lua function
		NRet:    len(l.outputs),                  // number of returned values
		Protect: true,                            // return err or panic
	}, jsonInputs...); err != nil {
		return err
	}

	for i := range l.outputs {
		err := json.Unmarshal([]byte(l.state.Get(-1).String()), l.outputs[i])

		if err != nil {
			return err
		}

		l.state.Pop(1)
	}

	return nil
}

func HTTPRequest(l *lua.LState) int {
	url := l.ToString(1)
	method := l.ToString(2)
	reqBodyString := l.ToString(3)

	httpClient := http.Client{
		Timeout: time.Second * 2, // Maximum of 2 secs
	}

	var req *http.Request
	var err error

	if method != "GET" && reqBodyString != "" {
		req, err = http.NewRequest(method, url, bytes.NewBuffer([]byte(reqBodyString)))
	} else {
		req, err = http.NewRequest(method, url, nil)
	}

	if err != nil {
		logrus.WithError(err).Error("Make new request")
		return 0
	}

	req.Header.Set("User-Agent", "assetto-server-manager")

	res, err := httpClient.Do(req)
	if err != nil {
		logrus.WithError(err).Error("do request")
		return 0
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		logrus.WithError(err).Error("read response body")
		return 0
	}

	l.Push(lua.LString(body))
	l.Push(lua.LNumber(res.StatusCode))

	return 2
}

func managerStartPlugin() error {
	p := NewPlugin()

	err := p.Call("./plugins/manager.lua", "onManagerStart")

	if err != nil {
		return err
	}

	return nil
}
