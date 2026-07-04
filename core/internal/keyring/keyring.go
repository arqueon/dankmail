// Package keyring stores account secrets (OAuth token JSON, IMAP/SMTP
// passwords) in the system keyring via the Secret Service API. Secrets
// never touch config files, the database, or logs.
package keyring

import "github.com/zalando/go-keyring"

const service = "dankmail"

// Well-known secret keys per account.
const (
	KeyOAuthToken   = "oauth-token"   // JSON-serialized oauth2.Token
	KeyIMAPPassword = "imap-password" // password or app-password
	KeySMTPPassword = "smtp-password" // only if different from IMAP
)

func key(accountID, name string) string { return accountID + "/" + name }

func Set(accountID, name, secret string) error {
	return keyring.Set(service, key(accountID, name), secret)
}

func Get(accountID, name string) (string, error) {
	return keyring.Get(service, key(accountID, name))
}

// Delete removes one secret; ErrNotFound is not an error for callers
// cleaning up an account.
func Delete(accountID, name string) error {
	err := keyring.Delete(service, key(accountID, name))
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}
