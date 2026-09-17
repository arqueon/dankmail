# Gmail synchronization and quota limits

DankMail paces Gmail requests by their documented quota cost. Each account's
provider uses a shared budget of 2,400 units per minute (40 units/second), below
Google's 6,000-unit per-user/per-project limit. A full thread costs 40 units.
The headroom accommodates another desktop using the same OAuth project;
additional clients can still exhaust Google's shared quota.

The budget and server cooldowns are kept per account for the lifetime of the
process, including provider rebuilds on `system.reload`. A daemon restart
starts a new in-memory budget; different accounts have independent budgets.

Reads retry rate-limit responses (429 and quota-specific 403) and selected
server errors at the failing request, up to five retries. Exponential backoff
starts at one second and adds jitter. `Retry-After`, including HTTP dates, is a
minimum delay. No individual admission waits more than three seconds inline;
retry cooldowns have a cumulative five-second budget per synchronization or
user action, shared across all its pages and threads. Normal quota pacing is
separate from that retry budget (a send costs 2.5 seconds of pacing).

Long waits return a retry hint immediately. The account's poll loop and
pending-operation queue schedule the next attempt no earlier than that hint.
Manual all-account sync and remote search continue to other accounts and
report failures afterwards. Short waits remain cancellable. Permission and
authentication errors are not treated as quota errors. Sends are paced but
never automatically replayed by this retry layer; failed writes also retain
the shared account cooldown.

Already fetched pages and threads remain in memory during retries. A successful
cycle reconciles them and advances the durable cursor. This is not a disk
checkpoint: stopping the daemon, deferring a long wait, or exhausting the
retry budget restarts from the last completed cursor. Full snapshots are never partially applied or used to
prune the local cache. Large imports can therefore take several minutes.

Polling and manual synchronization are serialized per account, including a
manual full-sync cursor reset. This avoids duplicate imports and cursor races.
OAuth tokens and user-supplied client credentials remain in the existing local
secret store; an upgrade does not require a new OAuth client or consent.

Source: [Gmail API usage limits](https://developers.google.com/workspace/gmail/api/reference/quota).
