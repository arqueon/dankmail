// Package oauth runs the OAuth 2.0 desktop (loopback) flow for Gmail
// accounts: it opens the consent URL in the browser, listens on a
// localhost redirect for the code, exchanges it, and hands the token to
// the keyring. Scopes are fixed to the minimum: gmail.modify + gmail.send
// — never the full https://mail.google.com/ scope.
package oauth

// GmailScopes is the exact, non-negotiable scope set (spec §3.2).
var GmailScopes = []string{
	"https://www.googleapis.com/auth/gmail.modify",
	"https://www.googleapis.com/auth/gmail.send",
}

// TODO(anillo1): Broker type — Start(ctx, accountEmail) → authURL,
// loopback listener on cfg.OAuthBindAddr, token exchange, refresh
// persistence via internal/keyring, reauth notification on refresh
// failure (errdefs.KindAuth).
