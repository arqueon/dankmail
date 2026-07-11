// Package oauth runs the OAuth 2.0 desktop (loopback) flow for Gmail
// accounts: it opens the consent URL in the browser, listens on a
// localhost redirect for the code, exchanges it, and hands the token to
// the keyring. Mail scopes stay minimal (gmail.modify + gmail.send —
// never the full https://mail.google.com/ scope); see GmailScopes for
// the read-only contacts extension.
package oauth

// GmailScopes is the exact scope set requested at consent. Mail access
// stays minimal per spec §3.2 (gmail.modify + gmail.send, NEVER full
// mailbox); the two read-only contacts scopes feed the compose
// autocomplete (People API) and were a deliberate, user-approved
// extension. Accounts consented before the extension keep working —
// only Google-contact suggestions require a re-consent.
var GmailScopes = []string{
	"https://www.googleapis.com/auth/gmail.modify",
	"https://www.googleapis.com/auth/gmail.send",
	"https://www.googleapis.com/auth/contacts.readonly",
	"https://www.googleapis.com/auth/contacts.other.readonly",
}

// GraphScopes is the Microsoft Graph equivalent, same minimality rule:
// mailbox read/write + send, the profile (for the account's address),
// and offline_access for the refresh token. Never Mail.ReadWrite.Shared
// or directory scopes.
var GraphScopes = []string{
	"https://graph.microsoft.com/Mail.ReadWrite",
	"https://graph.microsoft.com/Mail.Send",
	"https://graph.microsoft.com/User.Read",
	"offline_access",
}
