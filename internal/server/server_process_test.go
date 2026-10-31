package server

import (
	"errors"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"

	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

const fakeACServerEnv = "SERVER_MANAGER_TEST_FAKE_ACSERVER"

func TestMain(m *testing.M) {
	// the test binary doubles as a fake acServer, which just runs until it is killed
	if os.Getenv(fakeACServerEnv) == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func setupServerProcessTest(t *testing.T, executablePath string) (*AssettoServerProcess, string, func()) {
	dir, err := ioutil.TempDir("", "server-process")

	if err != nil {
		t.Fatal(err)
	}

	env := &serverenv.Env{
		InstallPath:  func() string { return dir },
		PanicCapture: func(fn func()) { fn() },
	}
	config := &smconfig.Configuration{}
	config.Steam.ExecutablePath = executablePath

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "store-shared"))

	if err := os.Setenv(fakeACServerEnv, "1"); err != nil {
		t.Fatal(err)
	}

	udpPort, err := FreeUDPPort()

	if err != nil {
		t.Fatal(err)
	}

	sp := NewAssettoServerProcess(config, func(udp.Message) {}, store, NewContentManagerWrapper(config, store, nil, nil, env), env, ioutil.Discard)

	return sp, fmt.Sprintf("127.0.0.1:%d", udpPort), func() {
		_ = os.Unsetenv(fakeACServerEnv)
		_ = os.RemoveAll(dir)
	}
}

func freeUDPPort(t *testing.T) int {
	port, err := FreeUDPPort()

	if err != nil {
		t.Fatal(err)
	}

	return port
}

// Stopping the server straight after starting it must not panic, the process has to exist by the time Start returns.
func TestAssettoServerProcess_StopRightAfterStart(t *testing.T) {
	executable, err := os.Executable()

	if err != nil {
		t.Fatal(err)
	}

	sp, udpAddress, cleanup := setupServerProcessTest(t, executable)
	defer cleanup()

	if err := sp.Start(smmodel.QuickRace{}, udpAddress, freeUDPPort(t), "", -1); err != nil {
		t.Fatal(err)
	}

	if !sp.IsRunning() {
		t.Fatal("expected the server to be running")
	}

	if err := sp.Stop(); err != nil {
		t.Logf("stop returned: %s", err)
	}

	if sp.IsRunning() {
		t.Error("expected the server to be stopped")
	}
}

// A server executable that can't be started must be reported, and must not leave the process in a running state.
func TestAssettoServerProcess_StartMissingExecutable(t *testing.T) {
	sp, udpAddress, cleanup := setupServerProcessTest(t, "does-not-exist-acServer")
	defer cleanup()

	if err := sp.Start(smmodel.QuickRace{}, udpAddress, freeUDPPort(t), "", -1); err == nil {
		t.Fatal("expected an error starting a missing executable")
	}

	if sp.IsRunning() {
		t.Error("expected the server not to be running")
	}

	if err := sp.Stop(); err != nil {
		t.Errorf("stopping a server that never started errored: %s", err)
	}

	// the UDP listener has been released, so the server can be started again
	if err := sp.Start(smmodel.QuickRace{}, udpAddress, freeUDPPort(t), "", -1); err == nil {
		t.Fatal("expected an error starting a missing executable")
	}
}

// A failing start check keeps the event from starting, and leaves a running event alone.
func TestAssettoServerProcess_StartCheck(t *testing.T) {
	executable, err := os.Executable()

	if err != nil {
		t.Fatal(err)
	}

	sp, udpAddress, cleanup := setupServerProcessTest(t, executable)
	defer cleanup()

	errRefused := errors.New("ports in use")
	sp.SetStartCheck(func() error { return errRefused })

	if err := sp.Start(smmodel.QuickRace{}, udpAddress, freeUDPPort(t), "", -1); err != errRefused {
		t.Fatalf("expected the error of the start check, got %v", err)
	}

	if sp.IsRunning() {
		t.Fatal("expected the server not to be started")
	}

	sp.SetStartCheck(func() error { return nil })

	if err := sp.Start(smmodel.QuickRace{}, udpAddress, freeUDPPort(t), "", -1); err != nil {
		t.Fatal(err)
	}

	defer func() { _ = sp.Stop() }()

	sp.SetStartCheck(func() error { return errRefused })

	if err := sp.Start(smmodel.QuickRace{}, udpAddress, freeUDPPort(t), "", -1); err != errRefused {
		t.Fatalf("expected the error of the start check, got %v", err)
	}

	if !sp.IsRunning() {
		t.Error("a refused start must not stop the running event")
	}
}
