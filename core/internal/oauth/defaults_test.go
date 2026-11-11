package oauth

import (
	"os"
	"path/filepath"
	"testing"

	zkeyring "github.com/zalando/go-keyring"
)

func TestParseClientJSONShapes(t *testing.T) {
	installed := []byte(`{"installed":{"client_id":"cid-1","client_secret":"csec-1"}}`)
	c, err := ParseClientJSON(installed)
	if err != nil || c.ClientID != "cid-1" || c.ClientSecret != "csec-1" {
		t.Fatalf("installed shape failed: %+v, err=%v", c, err)
	}

	flatSnake := []byte(`{"client_id":"cid-2","client_secret":"csec-2","token":"ignored"}`)
	c, err = ParseClientJSON(flatSnake)
	if err != nil || c.ClientID != "cid-2" || c.ClientSecret != "csec-2" {
		t.Fatalf("flat snake_case shape failed: %+v, err=%v", c, err)
	}

	flatCamel := []byte(`{"clientId":"cid-3","clientSecret":"csec-3"}`)
	c, err = ParseClientJSON(flatCamel)
	if err != nil || c.ClientID != "cid-3" || c.ClientSecret != "csec-3" {
		t.Fatalf("flat camelCase shape failed: %+v, err=%v", c, err)
	}

	web := []byte(`{"web":{"client_id":"cid-web","client_secret":"csec-web"}}`)
	if _, err := ParseClientJSON(web); err == nil {
		t.Fatal("expected web client JSON to be rejected")
	}
}

func TestSaveAndResolveDefaultClients(t *testing.T) {
	zkeyring.MockInit()
	tmp := filepath.Join(t.TempDir(), "oauth-clients.json")
	prevHook := clientsPathHook
	clientsPathHook = func() string { return tmp }
	defer func() { clientsPathHook = prevHook }()

	if err := SaveDefaultClientCreds("google", ClientCreds{ClientID: "gid", ClientSecret: "gsecret"}); err != nil {
		t.Fatalf("SaveDefaultClientCreds google: %v", err)
	}
	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat fallback file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}

	got, err := ResolveGoogleClient(ClientCreds{}, "", "")
	if err != nil || got.ClientID != "gid" || got.ClientSecret != "gsecret" {
		t.Fatalf("ResolveGoogleClient got %+v, err=%v", got, err)
	}

	// Explicit wins over stored default.
	got, err = ResolveGoogleClient(ClientCreds{ClientID: "exp-id", ClientSecret: "exp-sec"}, "", "")
	if err != nil || got.ClientID != "exp-id" {
		t.Fatalf("explicit should win, got %+v, err=%v", got, err)
	}

	if err := SaveDefaultClientCreds("microsoft", ClientCreds{ClientID: "ms-client-uuid"}); err != nil {
		t.Fatalf("SaveDefaultClientCreds microsoft: %v", err)
	}
	msGot, err := ResolveMicrosoftClient("", "")
	if err != nil || msGot.ClientID != "ms-client-uuid" {
		t.Fatalf("ResolveMicrosoftClient got %+v, err=%v", msGot, err)
	}
}
