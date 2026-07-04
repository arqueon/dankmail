// Package paths centralizes XDG base-directory locations, mirrored by
// quickshell/Common/Paths.qml on the UI side.
package paths

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

const appDir = "dankmail"

func ConfigDir() string { return filepath.Join(xdg.ConfigHome, appDir) }
func DataDir() string   { return filepath.Join(xdg.DataHome, appDir) }
func CacheDir() string  { return filepath.Join(xdg.CacheHome, appDir) }
func StateDir() string  { return filepath.Join(xdg.StateHome, appDir) }

// DatabasePath is the default SQLite location (overridable via DMAIL_DB_PATH).
func DatabasePath() string { return filepath.Join(DataDir(), "dankmail.db") }

// SocketPath is the fixed IPC unix socket. A single daemon per session is
// assumed; the daemon refuses to start if the socket is alive.
func SocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "dankmail.sock")
	}
	return filepath.Join(os.TempDir(), "dankmail.sock")
}

// UISettingsPath is the JSON settings file owned by the QML frontend.
func UISettingsPath() string { return filepath.Join(ConfigDir(), "ui-settings.json") }
