// Package paths centralizes XDG base-directory locations, mirrored by
// quickshell/Common/Paths.qml on the UI side.
//
// New installations use "duxmail" directories and "duxmail.sock". Existing
// installations that still have "dankmail" XDG directories or "dankmail.db"
// continue using them transparently so accounts, OAuth clients, settings,
// and cached threads survive upgrading without manual migration.
package paths

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

const (
	appDir       = "duxmail"
	legacyAppDir = "dankmail"

	dbFilename       = "duxmail.db"
	legacyDBFilename = "dankmail.db"

	socketFilename       = "duxmail.sock"
	legacySocketFilename = "dankmail.sock"
)

func xdgBase(envKey, fallback string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return fallback
}

func resolveAppDir(base string) string {
	primary := filepath.Join(base, appDir)
	if info, err := os.Stat(primary); err == nil && info.IsDir() {
		return primary
	}
	legacy := filepath.Join(base, legacyAppDir)
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		return legacy
	}
	return primary
}

func ConfigDir() string { return resolveAppDir(xdgBase("XDG_CONFIG_HOME", xdg.ConfigHome)) }
func DataDir() string   { return resolveAppDir(xdgBase("XDG_DATA_HOME", xdg.DataHome)) }
func CacheDir() string  { return resolveAppDir(xdgBase("XDG_CACHE_HOME", xdg.CacheHome)) }
func StateDir() string  { return resolveAppDir(xdgBase("XDG_STATE_HOME", xdg.StateHome)) }

// DatabasePath is the default SQLite location (overridable via DMAIL_DB_PATH).
// Reuses dankmail.db if present when duxmail.db has not been created.
func DatabasePath() string {
	dir := DataDir()
	primary := filepath.Join(dir, dbFilename)
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	legacy := filepath.Join(dir, legacyDBFilename)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return primary
}

func runtimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return os.TempDir()
}

// SocketPath is the primary IPC unix socket owned by the daemon.
// Honors DMAIL_SOCKET when explicitly set.
func SocketPath() string {
	if env := os.Getenv("DMAIL_SOCKET"); env != "" {
		return env
	}
	return filepath.Join(runtimeDir(), socketFilename)
}

// LegacySocketPath returns the pre-v0.5 socket path so the daemon can
// expose a compatibility symlink for older widgets or scripts. Returns ""
// when DMAIL_SOCKET overrides the socket location.
func LegacySocketPath() string {
	if os.Getenv("DMAIL_SOCKET") != "" {
		return ""
	}
	return filepath.Join(runtimeDir(), legacySocketFilename)
}

// DialSocketPath returns SocketPath() when present, falling back to
// LegacySocketPath() if an older daemon instance is still running.
func DialSocketPath() string {
	primary := SocketPath()
	if _, err := os.Lstat(primary); err == nil {
		return primary
	}
	if legacy := LegacySocketPath(); legacy != "" {
		if _, err := os.Lstat(legacy); err == nil {
			return legacy
		}
	}
	return primary
}

// UISettingsPath is the JSON settings file owned by the QML frontend.
func UISettingsPath() string { return filepath.Join(ConfigDir(), "ui-settings.json") }

// SettingsPath is the daemon's own settings file (notification actions,
// snooze default, …).
func SettingsPath() string { return filepath.Join(ConfigDir(), "settings.json") }

// DraftsPath is the local drafts persistence file.
func DraftsPath() string { return filepath.Join(DataDir(), "drafts.json") }

// AttachmentsCacheDir holds on-demand downloaded message attachments.
func AttachmentsCacheDir() string { return filepath.Join(CacheDir(), "attachments") }

// OAuthClientsPath stores reusable desktop OAuth client IDs/secrets when the
// desktop keyring is locked or unavailable (0600 permissions).
func OAuthClientsPath() string { return filepath.Join(DataDir(), "oauth-clients.json") }
