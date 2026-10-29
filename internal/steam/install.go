package steam

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sirupsen/logrus"

	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
)

// assettoServerSteamID is the ID of the server on steam.
const assettoServerSteamID = "302550"

var ErrNoSteamCMD = errors.New("servermanager: steamcmd was not found in $PATH")

// IsAssettoInstalled returns true if the Assetto Corsa server is installed in installPath.
func IsAssettoInstalled(installPath string) bool {
	_, err := os.Stat(filepath.Join(installPath, "system"))

	return err == nil
}

// InstallAssettoCorsaServer takes a steam login and password and runs steamcmd to install the assetto server into
// installPath. If installPath already contains the server, this function will exit without installing - unless
// force == true.
func InstallAssettoCorsaServer(installPath, login, password string, force bool) error {
	_, err := os.Stat(filepath.Join(installPath, "system"))

	if err != nil && !os.IsNotExist(err) {
		return err
	} else if !force && !os.IsNotExist(err) {
		return nil // server is installed
	}

	logrus.Infof("Attempting to install the Assetto Corsa Server (steamid: %s) to %s", assettoServerSteamID, installPath)

	commandToUse := "steamcmd.sh"

	if !isCommandAvailable(commandToUse) {
		if isCommandAvailable("steamcmd") {
			logrus.Infof("WARNING using steamcmd instead of steamcmd.sh. You must have run steamcmd before using this tool or Assetto Corsa Server will not install correctly.")
			commandToUse = "steamcmd"
		} else {
			return ErrNoSteamCMD
		}
	}

	cmd := exec.Command(commandToUse,
		"+@sSteamCmdForcePlatformType windows",
		fmt.Sprintf("+login %s %s", login, password),
		"+force_install_dir "+installPath,
		"+app_update "+assettoServerSteamID,
		"+quit",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	if err != nil {
		return err
	}

	// create default skins
	for _, f := range smcontent.DefaultSkinsLayout {
		err := os.MkdirAll(filepath.Join(installPath, filepath.FromSlash(f)), 0755)

		if err != nil {
			return err
		}
	}

	return nil
}

func isCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
