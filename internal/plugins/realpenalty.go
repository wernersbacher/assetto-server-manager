package plugins

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/sirupsen/logrus"
)

// handles configuration of the realpenalty plugin
// https://www.racedepartment.com/downloads/real-penalty-tool.29591/

const (
	realPenaltyBaseFolderName = "realpenalty"
)

func RealPenaltyExecutablePath(installPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(RealPenaltyFolderPath(installPath), "ac_penalty.exe")
	}

	return filepath.Join(RealPenaltyFolderPath(installPath), "ac_penalty")
}

func FixRealPenaltyExecutablePermissions(installPath string) error {
	if runtime.GOOS == "linux" {
		return os.Chmod(RealPenaltyExecutablePath(installPath), 0755)
	}

	return nil
}

func RealPenaltyFolderPath(installPath string) string {
	return filepath.Join(installPath, realPenaltyBaseFolderName)
}

func IsRealPenaltyInstalled(installPath string) bool {
	if _, err := os.Stat(RealPenaltyExecutablePath(installPath)); os.IsNotExist(err) {
		return false
	} else if err != nil {
		logrus.WithError(err).Error("Could not determine if realpenalty is enabled")
		return false
	} else {
		return true
	}
}
