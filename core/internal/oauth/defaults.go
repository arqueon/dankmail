package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/arqueon/dux-mail/core/internal/keyring"
	"github.com/arqueon/dux-mail/core/internal/paths"
)

// Compile-time OAuth client defaults, optionally injected via -ldflags -X
// (matching dankcalendar's build-time credential mechanism).
var (
	builtinGoogleClientID     string
	builtinGoogleClientSecret string
	builtinMicrosoftClientID  string
)

var (
	defaultsFileMu  sync.Mutex
	clientsPathHook = func() string {
		if p := strings.TrimSpace(os.Getenv("DMAIL_OAUTH_CLIENTS_PATH")); p != "" {
			return p
		}
		return paths.OAuthClientsPath()
	}
)

// BuiltinGoogleCredentials returns compile-time Google OAuth desktop credentials, if set.
func BuiltinGoogleCredentials() ClientCreds {
	return ClientCreds{
		ClientID:     strings.TrimSpace(builtinGoogleClientID),
		ClientSecret: strings.TrimSpace(builtinGoogleClientSecret),
	}
}

// BuiltinMicrosoftCredentials returns compile-time Microsoft OAuth public client credentials, if set.
func BuiltinMicrosoftCredentials() ClientCreds {
	return ClientCreds{
		ClientID: strings.TrimSpace(builtinMicrosoftClientID),
	}
}

func normalizeProviderKey(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "google", "gmail":
		return "google", nil
	case "microsoft", "outlook", "office365":
		return "microsoft", nil
	default:
		return "", fmt.Errorf("unsupported OAuth provider %q", provider)
	}
}

// SaveDefaultClientCreds stores reusable provider-wide OAuth client credentials
// in the system keyring (when available) and in a 0600 local fallback file so
// both GUI and headless CLI sessions can reuse them across accounts.
func SaveDefaultClientCreds(provider string, creds ClientCreds) error {
	key, err := normalizeProviderKey(provider)
	if err != nil {
		return err
	}
	creds.ClientID = strings.TrimSpace(creds.ClientID)
	creds.ClientSecret = strings.TrimSpace(creds.ClientSecret)
	if creds.ClientID == "" {
		return errors.New("oauth client ID is required")
	}
	if key == "google" && creds.ClientSecret == "" {
		return errors.New("google oauth client secret is required")
	}

	raw, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	keyringErr := keyring.Set("provider/"+key, keyring.KeyOAuthClient, string(raw))
	fileErr := writeDefaultClientFile(key, creds)
	if fileErr != nil && keyringErr != nil {
		return fmt.Errorf("store default oauth client: %w", fileErr)
	}
	return nil
}

// LoadDefaultClientCreds loads reusable provider-wide OAuth client credentials
// from the local store or system keyring.
func LoadDefaultClientCreds(provider string) (ClientCreds, error) {
	key, err := normalizeProviderKey(provider)
	if err != nil {
		return ClientCreds{}, err
	}
	if raw, err := keyring.Get("provider/"+key, keyring.KeyOAuthClient); err == nil && raw != "" {
		var c ClientCreds
		if json.Unmarshal([]byte(raw), &c) == nil && c.ClientID != "" {
			return c, nil
		}
	}
	return readDefaultClientFile(key)
}

func writeDefaultClientFile(providerKey string, creds ClientCreds) error {
	defaultsFileMu.Lock()
	defer defaultsFileMu.Unlock()

	p := clientsPathHook()
	all := map[string]ClientCreds{}
	if raw, err := os.ReadFile(p); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &all)
	}
	all[providerKey] = creds
	out, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func readDefaultClientFile(providerKey string) (ClientCreds, error) {
	defaultsFileMu.Lock()
	defer defaultsFileMu.Unlock()

	p := clientsPathHook()
	raw, err := os.ReadFile(p)
	if err != nil {
		return ClientCreds{}, err
	}
	all := map[string]ClientCreds{}
	if err := json.Unmarshal(raw, &all); err != nil {
		return ClientCreds{}, err
	}
	c, ok := all[providerKey]
	if !ok || strings.TrimSpace(c.ClientID) == "" {
		return ClientCreds{}, fmt.Errorf("no default oauth client for %s", providerKey)
	}
	return c, nil
}

// ResolveGoogleClient resolves Google OAuth desktop credentials in priority order:
// 1. Explicit credentials (flag, UI input, or per-account stored credentials)
// 2. Provider-default stored credentials (dmail account setup google / prior wizard setup)
// 3. Existing Gmail accounts' stored credentials
// 4. Environment variables (DMAIL_GOOGLE_CLIENT_ID / DMAIL_GOOGLE_CLIENT_SECRET)
// 5. Compile-time built-in credentials
func ResolveGoogleClient(explicit ClientCreds, envID, envSecret string, accountIDs ...string) (ClientCreds, error) {
	if strings.TrimSpace(explicit.ClientID) != "" && strings.TrimSpace(explicit.ClientSecret) != "" {
		return ClientCreds{
			ClientID:     strings.TrimSpace(explicit.ClientID),
			ClientSecret: strings.TrimSpace(explicit.ClientSecret),
		}, nil
	}
	if def, err := LoadDefaultClientCreds("google"); err == nil && def.ClientID != "" && def.ClientSecret != "" {
		return def, nil
	}
	for _, id := range accountIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if c, err := LoadClientCreds(id); err == nil && c.ClientID != "" && c.ClientSecret != "" {
			return c, nil
		}
	}
	if strings.TrimSpace(envID) != "" && strings.TrimSpace(envSecret) != "" {
		return ClientCreds{
			ClientID:     strings.TrimSpace(envID),
			ClientSecret: strings.TrimSpace(envSecret),
		}, nil
	}
	if b := BuiltinGoogleCredentials(); b.ClientID != "" && b.ClientSecret != "" {
		return b, nil
	}
	return ClientCreds{}, errors.New("Google OAuth client credentials are not configured; run 'dmail account setup google --client-json <file>' or use the in-app setup guide")
}

// ResolveMicrosoftClient resolves Microsoft OAuth public-client credentials in priority order:
// 1. Explicit client ID
// 2. Provider-default stored client ID (dmail account setup microsoft / prior wizard setup)
// 3. Existing Microsoft accounts' stored client IDs
// 4. Environment variable (DMAIL_MICROSOFT_CLIENT_ID)
// 5. Compile-time built-in client ID
func ResolveMicrosoftClient(explicitID, envID string, accountIDs ...string) (ClientCreds, error) {
	if strings.TrimSpace(explicitID) != "" {
		return ClientCreds{ClientID: strings.TrimSpace(explicitID)}, nil
	}
	if def, err := LoadDefaultClientCreds("microsoft"); err == nil && def.ClientID != "" {
		return ClientCreds{ClientID: def.ClientID}, nil
	}
	for _, id := range accountIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if c, err := LoadClientCreds(id); err == nil && c.ClientID != "" {
			return ClientCreds{ClientID: c.ClientID}, nil
		}
	}
	if strings.TrimSpace(envID) != "" {
		return ClientCreds{ClientID: strings.TrimSpace(envID)}, nil
	}
	if b := BuiltinMicrosoftCredentials(); b.ClientID != "" {
		return b, nil
	}
	return ClientCreds{}, errors.New("Microsoft OAuth client ID is not configured; run 'dmail account setup microsoft --client-id <id>' or use the in-app setup guide")
}
