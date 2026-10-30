package contenthttp

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
)

func TestRemoveWeatherPreset(t *testing.T) {
	installPath, err := ioutil.TempDir("", "weather-delete")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(installPath)

	weatherPath := filepath.Join(installPath, "content", "weather")

	if err := os.MkdirAll(filepath.Join(weatherPath, "my_preset"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := removeWeatherPreset(installPath, "my_preset"); err != nil {
		t.Fatalf("removing an installed preset: %v", err)
	}

	if _, err := os.Stat(filepath.Join(weatherPath, "my_preset")); !os.IsNotExist(err) {
		t.Errorf("preset folder still exists: %v", err)
	}

	// built-in presets have no folder, they must not be reported as deleted
	for _, key := range []string{"3_clear", "missing", "", "..", "../weather"} {
		if err := removeWeatherPreset(installPath, key); err != errWeatherNotInstalled {
			t.Errorf("removeWeatherPreset(%q) = %v, want errWeatherNotInstalled", key, err)
		}
	}

	if _, err := os.Stat(weatherPath); err != nil {
		t.Errorf("weather folder must not be removed: %v", err)
	}
}

func TestInstalledWeatherPresets(t *testing.T) {
	installPath, err := ioutil.TempDir("", "weather-installed")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(installPath)

	if err := os.MkdirAll(filepath.Join(installPath, "content", "weather", "my_preset"), 0755); err != nil {
		t.Fatal(err)
	}

	listed := smcontent.Weather{"3_clear": "Clear", "my_preset": "My Preset"}
	installed := installedWeatherPresets(installPath, listed)

	if installed["3_clear"] || !installed["my_preset"] {
		t.Fatalf("expected only my_preset to be installed, got %v", installed)
	}
}
