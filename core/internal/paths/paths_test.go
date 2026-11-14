package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathsFreshInstallUsesDuxmail(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "run"))
	t.Setenv("DMAIL_SOCKET", "")

	if got, want := DataDir(), filepath.Join(tmp, "data", "duxmail"); got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
	if got, want := ConfigDir(), filepath.Join(tmp, "config", "duxmail"); got != want {
		t.Fatalf("ConfigDir() = %q, want %q", got, want)
	}
	if got, want := DatabasePath(), filepath.Join(tmp, "data", "duxmail", "duxmail.db"); got != want {
		t.Fatalf("DatabasePath() = %q, want %q", got, want)
	}
	if got, want := SocketPath(), filepath.Join(tmp, "run", "duxmail.sock"); got != want {
		t.Fatalf("SocketPath() = %q, want %q", got, want)
	}
}

func TestPathsFallbackToLegacyDankmail(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "run"))
	t.Setenv("DMAIL_SOCKET", "")

	legacyData := filepath.Join(tmp, "data", "dankmail")
	legacyCfg := filepath.Join(tmp, "config", "dankmail")
	runDir := filepath.Join(tmp, "run")
	for _, dir := range []string{legacyData, legacyCfg, runDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	legacyDB := filepath.Join(legacyData, "dankmail.db")
	if err := os.WriteFile(legacyDB, []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacySock := filepath.Join(runDir, "dankmail.sock")
	if err := os.WriteFile(legacySock, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := DataDir(); got != legacyData {
		t.Fatalf("DataDir() = %q, want legacy %q", got, legacyData)
	}
	if got := ConfigDir(); got != legacyCfg {
		t.Fatalf("ConfigDir() = %q, want legacy %q", got, legacyCfg)
	}
	if got := DatabasePath(); got != legacyDB {
		t.Fatalf("DatabasePath() = %q, want legacy %q", got, legacyDB)
	}
	if got := DialSocketPath(); got != legacySock {
		t.Fatalf("DialSocketPath() = %q, want legacy %q", got, legacySock)
	}
}
