package content

import (
	"github.com/wernersbacher/assetto-server-manager/internal/plainname"

	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cj123/ini"
	"github.com/sirupsen/logrus"
)

// LockedTyreSetupFolder is the folder, next to the track setups, which holds the generated tyre-locked setups of race weekend sessions.
const LockedTyreSetupFolder = "server_manager_locked_tyres"

// CarSetups is a map of car name to the setups for that car.
// it is a map of car name => track name => []setup
type CarSetups map[string]map[string][]string

// ListAllSetups lists all car setups below the setups folder.
func ListAllSetups(installPath string) (CarSetups, error) {
	setupDirectory := filepath.Join(installPath, "setups")

	if _, err := os.Stat(setupDirectory); os.IsNotExist(err) {
		err := os.MkdirAll(setupDirectory, 0755)

		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	setups := make(CarSetups)

	// multi server installs may use symbolic links for setups folder, which filepath.Walk will ignore
	setupDirectory, err := filepath.EvalSymlinks(setupDirectory)

	if err != nil {
		setupDirectory = filepath.Join(installPath, "setups")
	}

	err = filepath.Walk(setupDirectory, func(path string, file os.FileInfo, _ error) error {
		if file.IsDir() || filepath.Ext(file.Name()) != ".ini" {
			return nil
		}

		// corner case of stray ini files winding up in the top level of the folder
		if filepath.ToSlash(filepath.Dir(path)) == filepath.ToSlash(setupDirectory) {
			return nil
		}

		// read the setup file to get the car name
		name, err := getCarNameFromSetup(path)

		if err != nil {
			logrus.WithError(err).Errorf("Could not get car name from setup file %s", file.Name())
			return nil
		}

		if setups[name] == nil {
			setups[name] = make(map[string][]string)
		}

		trackParts := strings.Split(filepath.ToSlash(filepath.Dir(path)), "/")

		if len(trackParts) > 0 {
			trackName := trackParts[len(trackParts)-1]

			if trackName == LockedTyreSetupFolder {
				return nil // don't list locked tyre setup folder
			}

			setups[name][trackName] = append(setups[name][trackName], file.Name())
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return setups, nil
}

// ListSetupsForCar lists the setups of a car (track name => setup files).
func ListSetupsForCar(installPath, model string) (map[string][]string, error) {
	setups := make(map[string][]string)

	setupDirectory := filepath.Join(installPath, "setups", model)

	if _, err := os.Stat(setupDirectory); os.IsNotExist(err) {
		return setups, nil
	} else if err != nil {
		return nil, err
	}

	err := filepath.Walk(setupDirectory, func(path string, file os.FileInfo, _ error) error {
		if file.IsDir() || filepath.Ext(file.Name()) != ".ini" {
			return nil
		}

		trackParts := strings.Split(filepath.ToSlash(filepath.Dir(path)), "/")

		if len(trackParts) > 0 {
			trackName := trackParts[len(trackParts)-1]

			if trackName == LockedTyreSetupFolder {
				return nil // don't list locked tyre setup folder
			}

			setups[trackName] = append(setups[trackName], file.Name())
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return setups, nil
}

func getCarNameFromSetup(setupFile interface{}) (string, error) {
	i, err := ini.Load(setupFile)

	if err != nil {
		return "", err
	}

	car, err := i.GetSection("CAR")

	if err != nil {
		return "", err
	}

	name, err := car.GetKey("MODEL")

	if err != nil {
		return "", err
	}

	return name.String(), nil
}

// UploadCarSetup stores the setup file uploaded in the "SetupFile" form field and returns the car it is for.
func UploadCarSetup(installPath string, r *http.Request) (string, error) {
	track := r.FormValue("Track")

	if !plainname.Valid(track) {
		return "", errors.New("servermanager: invalid setup track name")
	}

	uploadedFile, header, err := r.FormFile("SetupFile")

	if err != nil {
		return "", err
	}

	defer uploadedFile.Close()

	carName, err := getCarNameFromSetup(uploadedFile)

	if err != nil {
		return carName, err
	}

	_, err = uploadedFile.Seek(0, 0)

	if err != nil {
		return carName, err
	}

	// the car name comes from the uploaded file itself, so it needs checking too
	if !plainname.Valid(carName) || !plainname.Valid(header.Filename) {
		return "", errors.New("servermanager: invalid setup car or file name")
	}

	saveFilepath := filepath.Join(installPath, "setups", carName, track)

	if err := os.MkdirAll(saveFilepath, 0755); err != nil {
		return carName, err
	}

	savedFile, err := os.Create(filepath.Join(saveFilepath, header.Filename))

	if err != nil {
		return carName, err
	}

	defer savedFile.Close()

	_, err = io.Copy(savedFile, uploadedFile)

	return carName, err
}
