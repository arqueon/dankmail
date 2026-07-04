package sync

import "errors"

var (
	errNoProvider = errors.New("sync: no provider registered for account")
	errBadPayload = errors.New("sync: op payload is missing or malformed")
)
