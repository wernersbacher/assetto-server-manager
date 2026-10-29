package plugins

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/sirupsen/logrus"
)

// kissmyrank handles configuration of the kissmyrank plugin
// https://www.racedepartment.com/downloads/kissmyrank-local-assetto-corsa-server-plugin.17667/

const (
	kissMyRankBaseFolderName     = "kissmyrank"
	kissMyRankConfigJSONFileName = "config.json"
)

func KissMyRankExecutablePath(installPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(KissMyRankFolderPath(installPath), "ac_kissmyrank-win.exe")
	}

	return filepath.Join(KissMyRankFolderPath(installPath), "ac_kissmyrank-linux")
}

func FixKissMyRankExecutablePermissions(installPath string) error {
	if runtime.GOOS == "linux" {
		return os.Chmod(KissMyRankExecutablePath(installPath), 0755)
	}

	return nil
}

func KissMyRankFolderPath(installPath string) string {
	return filepath.Join(installPath, kissMyRankBaseFolderName)
}

func KissMyRankConfigPath(installPath string) string {
	return filepath.Join(KissMyRankFolderPath(installPath), kissMyRankConfigJSONFileName)
}

// IsKissMyRankInstalled looks in the installPath for a "kissmyrank" directory with the correct kissmyrank executable for the given platform
func IsKissMyRankInstalled(installPath string) bool {
	if _, err := os.Stat(KissMyRankExecutablePath(installPath)); os.IsNotExist(err) {
		return false
	} else if err != nil {
		logrus.WithError(err).Error("Could not determine if kissmyrank is enabled")
		return false
	} else {
		return true
	}
}
