# Gmail synchronization and quota limits

DankMail paces Gmail requests by their documented quota cost. Each account's
provider uses a shared budget of 2,400 units per minute (40 units/second), below
Google's 6,000-unit per-user/per-project limit. A full thread costs 40 units.
The headroom accommodates another desktop using the same OAuth project;
additional clients can still exhaust Google's shared quota.

Reads retry rate-limit responses (429 and quota-specific 403) and selected
server errors at the failing request, up to five retries. Quota retries wait
at least 60 seconds, then 120 seconds, with jitter; server retries use
exponential backoff. A longer `Retry-After` is honored. Waits are cancellable
and the cooldown is shared with other requests for that account. Permission
and authentication errors are not treated as quota errors. Sends are paced
but never automatically replayed by this retry layer.

Already fetched pages and threads remain in memory during retries. A successful
cycle reconciles them and advances the durable cursor. This is not a disk
checkpoint: stopping the daemon or exhausting all retries restarts from the
last completed cursor. Full snapshots are never partially applied or used to
prune the local cache. Large imports can therefore take several minutes.

Polling and manual synchronization are serialized per account, including a
manual full-sync cursor reset. This avoids duplicate imports and cursor races.
OAuth tokens and user-supplied client credentials remain in the existing local
secret store; an upgrade does not require a new OAuth client or consent.

Source: [Gmail API usage limits](https://developers.google.com/workspace/gmail/api/reference/quota).
