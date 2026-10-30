// Package config holds the server manager configuration (config.yml).
package config

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cj123/sessions"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type Configuration struct {
	HTTP          HTTPConfig          `yaml:"http"`
	Steam         SteamConfig         `yaml:"steam"`
	Servers       []ServerSlotConfig  `yaml:"servers"`
	Store         StoreConfig         `yaml:"store"`
	LiveMap       LiveMapConfig       `yaml:"live_map"`
	Server        ServerExtraConfig   `yaml:"server"`
	Accounts      AccountsConfig      `yaml:"accounts"`
	Monitoring    MonitoringConfig    `yaml:"monitoring"`
	Championships ChampionshipsConfig `yaml:"championships"`
	Lua           LuaConfig           `yaml:"lua"`
}

type ChampionshipsConfig struct {
	RecaptchaConfig struct {
		SiteKey   string `yaml:"site_key"`
		SecretKey string `yaml:"secret_key"`
	} `yaml:"recaptcha"`
}

type MonitoringConfig struct {
	Enabled bool `yaml:"enabled"`
}

type AccountsConfig struct {
	AdminPasswordOverride string `yaml:"admin_password_override"`
}

type LiveMapConfig struct {
	IntervalMs int `yaml:"refresh_interval_ms"`
}

func (l *LiveMapConfig) IsEnabled() bool {
	return l.IntervalMs > 0
}

type HTTPConfig struct {
	Hostname         string `yaml:"hostname"`
	SessionKey       string `yaml:"session_key"`
	SessionStoreType string `yaml:"session_store_type"`
	SessionStorePath string `yaml:"session_store_path"`
	BaseURL          string `yaml:"server_manager_base_URL"`

	TLS TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertPath string `yaml:"cert_path"`
	KeyPath  string `yaml:"key_path"`
}

type LuaConfig struct {
	Enabled bool `yaml:"enabled"`
}

const (
	sessionStoreCookie     = "cookie"
	sessionStoreFilesystem = "filesystem"
)

// CreateSessionStore builds the session store configured in HTTPConfig.
func (h *HTTPConfig) CreateSessionStore() (sessions.Store, error) {
	switch h.SessionStoreType {
	case sessionStoreFilesystem:
		if info, err := os.Stat(h.SessionStorePath); os.IsNotExist(err) {
			err := os.MkdirAll(h.SessionStorePath, 0755)

			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		} else if !info.IsDir() {
			return nil, errors.New("config: session store location must be a directory")
		}

		fsStore := sessions.NewFilesystemStore(h.SessionStorePath, []byte(h.SessionKey))
		fsStore.Options.SameSite = http.SameSiteStrictMode

		return fsStore, nil
	case sessionStoreCookie:
		fallthrough
	default:
		cookieStore := sessions.NewCookieStore([]byte(h.SessionKey))
		cookieStore.Options.SameSite = http.SameSiteStrictMode

		return cookieStore, nil
	}
}

type SteamConfig struct {
	Username       string `yaml:"username"`
	Password       string `yaml:"password"`
	InstallPath    string `yaml:"install_path"`
	ForceUpdate    bool   `yaml:"force_update"`
	ExecutablePath string `yaml:"executable_path"`
}

// ServerSlotConfig is one entry of the servers list: an acServer with its own working directory.
type ServerSlotConfig struct {
	ID             string `yaml:"id"`
	Name           string `yaml:"name"`
	InstallPath    string `yaml:"install_path"`
	ExecutablePath string `yaml:"executable_path"`
}

// DefaultSlotID is the ID of the slot that is derived from the steam section when no servers are configured. It is
// empty on purpose: the ServerID of that slot is the one from the store (InitServerID), not a configured one.
const DefaultSlotID = ""

// DefaultSlotName is the name of the slot that is derived from the steam section.
const DefaultSlotName = "Server 1"

// Slot is an effective server slot: one acServer with its own working directory.
type Slot struct {
	// ID is the configured id of the slot, DefaultSlotID for the slot derived from the steam section.
	ID   string
	Name string

	// InstallPath is the absolute working directory of the acServer.
	InstallPath string

	// ExecutablePath is the acServer executable, relative to InstallPath unless it is absolute.
	ExecutablePath string
}

// IsDefault returns true for the slot that is derived from the steam section.
func (s Slot) IsDefault() bool {
	return s.ID == DefaultSlotID
}

// Slots returns the effective server slots. If no servers are configured, exactly one default slot is derived from
// steam.install_path and steam.executable_path. Install paths are made absolute by joining relative ones to the
// working directory; the first slot is the one the content is managed through.
func (c *Configuration) Slots() ([]Slot, error) {
	executablePath := c.Steam.ExecutablePath

	if executablePath == "" {
		executablePath = ServerExecutablePath
	}

	if len(c.Servers) == 0 {
		installPath, err := absInstallPath(c.Steam.InstallPath)

		if err != nil {
			return nil, err
		}

		return []Slot{{
			ID:             DefaultSlotID,
			Name:           DefaultSlotName,
			InstallPath:    installPath,
			ExecutablePath: executablePath,
		}}, nil
	}

	slots := make([]Slot, 0, len(c.Servers))

	for i, server := range c.Servers {
		if server.ID == "" {
			return nil, fmt.Errorf("config: servers[%d] has no id", i)
		}

		if server.InstallPath == "" {
			return nil, fmt.Errorf("config: server %q has no install_path", server.ID)
		}

		installPath, err := absInstallPath(server.InstallPath)

		if err != nil {
			return nil, err
		}

		slot := Slot{
			ID:             server.ID,
			Name:           server.Name,
			InstallPath:    installPath,
			ExecutablePath: server.ExecutablePath,
		}

		if slot.Name == "" {
			slot.Name = slot.ID
		}

		if slot.ExecutablePath == "" {
			slot.ExecutablePath = executablePath
		}

		for _, other := range slots {
			if other.ID == slot.ID {
				return nil, fmt.Errorf("config: server id %q is used more than once", slot.ID)
			}

			if samePath(other.InstallPath, slot.InstallPath) {
				return nil, fmt.Errorf("config: servers %q and %q have the same install_path (%s)", other.ID, slot.ID, slot.InstallPath)
			}
		}

		slots = append(slots, slot)
	}

	return slots, nil
}

// absInstallPath makes an install path absolute by joining a relative one to the working directory.
func absInstallPath(installPath string) (string, error) {
	if filepath.IsAbs(installPath) {
		return filepath.Clean(installPath), nil
	}

	wd, err := os.Getwd()

	if err != nil {
		return "", fmt.Errorf("config: unable to get working directory: %v", err)
	}

	return filepath.Join(wd, installPath), nil
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}

	return a == b
}

type StoreConfig struct {
	Type                    string        `yaml:"type"`
	Path                    string        `yaml:"path"`
	SharedPath              string        `yaml:"shared_data_path"`
	ScheduledEventCheckLoop time.Duration `yaml:"scheduled_event_check_loop"`
}

type ServerExtraConfig struct {
	Plugins                     []*CommandPlugin `yaml:"plugins"`
	AuditLogging                bool             `yaml:"audit_logging"`
	PerformanceMode             bool             `yaml:"performance_mode"`
	DisableWindowsBrowserOpen   bool             `yaml:"dont_open_browser"`
	ScanContentFolderForChanges bool             `yaml:"scan_content_folder_for_changes"`
	UseCarNameCache             bool             `yaml:"use_car_name_cache"`
	PersistMidSessionResults    bool             `yaml:"persist_mid_session_results"`

	// Deprecated: use Plugins instead
	RunOnStart []string `yaml:"run_on_start"`
}

type CommandPlugin struct {
	Executable string   `yaml:"executable"`
	Arguments  []string `yaml:"arguments"`
}

func (c *CommandPlugin) String() string {
	out := c.Executable
	out += strings.Join(c.Arguments, " ")

	return out
}

func ReadConfig(location string) (conf *Configuration, err error) {
	f, err := os.Open(location)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	if err := yaml.NewDecoder(f).Decode(&conf); err != nil {
		return nil, err
	}

	if conf.Accounts.AdminPasswordOverride != "" {
		logrus.Infof("WARNING! Admin Password Override is set. Please only have this set if you are resetting your admin account password!")
	}

	if conf.Steam.ExecutablePath == "" {
		conf.Steam.ExecutablePath = ServerExecutablePath
	}

	if _, err := conf.Slots(); err != nil {
		return nil, err
	}

	return conf, nil
}
