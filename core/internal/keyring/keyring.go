// Package keyring stores account secrets (OAuth token JSON, IMAP/SMTP
// passwords) in the system keyring via the Secret Service API, with
// fallback to the database Secret table if the keyring is unavailable.
package keyring

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/account"
	entsecret "github.com/arqueon/dankmail/core/ent/secret"
)

const service = "dankmail"

// Well-known secret keys per account.
const (
	KeyOAuthToken   = "oauth-token"   // JSON-serialized oauth2.Token
	KeyOAuthClient  = "oauth-client"  // JSON {clientId, clientSecret} of the user's own OAuth app
	KeyIMAPPassword = "imap-password" // password or app-password
	KeySMTPPassword = "smtp-password" // only if different from IMAP
)

var (
	fallbackMu sync.RWMutex
	fallbackDB *ent.Client
)

// SetFallbackDB registers an ent.Client used as a fallback secret store
// when the desktop Secret Service / keyring daemon is unavailable.
func SetFallbackDB(db *ent.Client) {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()
	fallbackDB = db
}

func key(accountID, name string) string { return accountID + "/" + name }

func Set(accountID, name, secret string) error {
	err := keyring.Set(service, key(accountID, name), secret)
	if err == nil {
		deleteFallback(accountID, name)
		return nil
	}

	// Fallback to database if keyring is unavailable
	if ferr := setFallback(accountID, name, secret); ferr == nil {
		return nil
	}
	return err
}

func Get(accountID, name string) (string, error) {
	val, err := keyring.Get(service, key(accountID, name))
	if err == nil {
		return val, nil
	}

	// Check fallback DB
	if fval, ferr := getFallback(accountID, name); ferr == nil {
		return fval, nil
	}
	return "", err
}

// Delete removes one secret from both keyring and fallback DB; ErrNotFound is not an error for callers
// cleaning up an account.
func Delete(accountID, name string) error {
	deleteFallback(accountID, name)
	err := keyring.Delete(service, key(accountID, name))
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func setFallback(accountID, name, secret string) error {
	fallbackMu.RLock()
	db := fallbackDB
	fallbackMu.RUnlock()
	if db == nil {
		return errors.New("no fallback db configured")
	}

	acctUUID, err := uuid.Parse(accountID)
	if err != nil {
		return err
	}

	ctx := context.Background()
	existing, err := db.Secret.Query().
		Where(entsecret.HasAccountWith(account.IDEQ(acctUUID)), entsecret.KeyEQ(name)).
		Only(ctx)
	if ent.IsNotFound(err) {
		_, err = db.Secret.Create().
			SetAccountID(acctUUID).
			SetKey(name).
			SetValue([]byte(secret)).
			Save(ctx)
		return err
	} else if err != nil {
		return err
	}

	_, err = db.Secret.UpdateOne(existing).
		SetValue([]byte(secret)).
		Save(ctx)
	return err
}

func getFallback(accountID, name string) (string, error) {
	fallbackMu.RLock()
	db := fallbackDB
	fallbackMu.RUnlock()
	if db == nil {
		return "", errors.New("no fallback db configured")
	}

	acctUUID, err := uuid.Parse(accountID)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	sec, err := db.Secret.Query().
		Where(entsecret.HasAccountWith(account.IDEQ(acctUUID)), entsecret.KeyEQ(name)).
		Only(ctx)
	if err != nil {
		return "", err
	}
	return string(sec.Value), nil
}

func deleteFallback(accountID, name string) {
	fallbackMu.RLock()
	db := fallbackDB
	fallbackMu.RUnlock()
	if db == nil {
		return
	}

	acctUUID, err := uuid.Parse(accountID)
	if err != nil {
		return
	}

	ctx := context.Background()
	_, _ = db.Secret.Delete().
		Where(entsecret.HasAccountWith(account.IDEQ(acctUUID)), entsecret.KeyEQ(name)).
		Exec(ctx)
}
