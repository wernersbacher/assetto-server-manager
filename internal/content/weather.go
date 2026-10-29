package content

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/cj123/ini"
)

const weatherInfoFile = "weather.ini"

type Weather map[string]string

// ListWeather lists the weather presets installed on the server (folder name => display name).
func ListWeather(installPath string) (Weather, error) {
	// defaultWeather is loaded if there aren't any weather options on the server
	defaultWeather := Weather{
		"1_heavy_fog":    "Heavy Fog",
		"2_light_fog":    "Light Fog",
		"3_clear":        "Clear",
		"4_mid_clear":    "Mid Clear",
		"5_light_clouds": "Light Clouds",
		"6_mid_clouds":   "Mid Clouds",
		"7_heavy_clouds": "Heavy Clouds",
	}

	baseDir := filepath.Join(installPath, "content", "weather")

	weatherFolders, err := ioutil.ReadDir(baseDir)

	if os.IsNotExist(err) {
		return defaultWeather, nil
	} else if err != nil {
		return nil, err
	}

	weather := defaultWeather

	for _, weatherFolder := range weatherFolders {
		if !weatherFolder.IsDir() {
			continue
		}

		// read the weather info file
		name, err := getWeatherName(baseDir, weatherFolder.Name())

		if err != nil {
			return nil, err
		}

		if name == "" {
			name = weatherFolder.Name()
		}

		weather[weatherFolder.Name()] = name
	}

	if len(weather) == 0 {
		return defaultWeather, nil
	}

	return weather, nil
}

func getWeatherName(folder, weather string) (string, error) {
	f, err := os.Open(filepath.Join(folder, weather, weatherInfoFile))

	if err != nil {
		return "", nil
	}

	defer f.Close()

	i, err := ini.Load(f)

	if err != nil {
		return "", err
	}

	s, err := i.GetSection("LAUNCHER")

	if err != nil {
		return "", err
	}

	k, err := s.GetKey("NAME")

	if err != nil {
		return "", nil
	}

	return k.String(), nil
}

// GetWeatherType returns the CM weather type (WFX type) of a weather preset.
func GetWeatherType(installPath, weather string) (int, error) {
	baseDir := filepath.Join(installPath, "content", "weather")

	f, err := os.Open(filepath.Join(baseDir, weather, weatherInfoFile))

	if err != nil {
		return 0, err
	}

	defer f.Close()

	i, err := ini.Load(f)

	if err != nil {
		return 0, err
	}

	s, err := i.GetSection("__LAUNCHER_CM")

	if err != nil {
		return 0, err
	}

	k, err := s.GetKey("WEATHER_TYPE")

	if err != nil {
		return 0, nil
	}

	weatherType, err := k.Int()

	if err != nil {
		return 0, nil
	}

	return weatherType, nil
}
