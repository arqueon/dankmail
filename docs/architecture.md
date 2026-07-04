# dankmail architecture

dankmail is a multi-account mail **notifier with triage**, not a mail
client: read, mark, archive, trash, snooze, short reply. Anything richer
(attachments, HTML, folders, filters) opens the provider's webmail via
deep link. The architecture is deliberately modeled on
[dankcalendar](https://github.com/AvengeMedia/dankcalendar): a Go daemon
(`dmail`) owning sync, notifications, a unix-socket IPC and a
localhost-only HTTP API, plus a Quickshell/QML UI that is just a view
over the daemon. Closing the window hides it; sync keeps running
(systemd user unit, `graphical-session.target`).

## Providers and capabilities

Every backend implements `internal/provider.Provider` and declares a
static `Capability` bitmask (`CapModifyFlags`, `CapArchive`, `CapTrash`,
`CapSendReply`, `CapCompose`, `CapPush`, `CapDeepLink`,
`CapHistorySync`, reserved `CapServerSnooze`). The UI and the op queue
adapt to the declared set; calling an unsupported method is a bug, never
a runtime fallback. Two providers are planned:

- **Gmail (API, not IMAP).** OAuth desktop flow with loopback redirect;
  the user brings their own OAuth client (see `gmail-setup.md`). Scopes
  are exactly `gmail.modify` + `gmail.send` — never full mail access.
  Sync is `threads.list` initially, then incremental `history.list`
  from the persisted `historyId` (expired cursor ⇒ full resync).
  Triage maps to labels: read = −UNREAD, star = +STARRED,
  archive = −INBOX, trash = +TRASH (`batchModify` for homogeneous
  batches). Deep links use `#all/{threadId}` + `authuser=` with
  `#search/rfc822msgid:` as fallback. Replies are plain-text MIME with
  `In-Reply-To`/`References` and the original `threadId`.
- **Generic IMAP.** App-password auth (keyring), RFC 6154 SPECIAL-USE
  for Archive/Trash/Sent with heuristic + per-account override,
  `\Seen`/`\Flagged` flags, MOVE for archive/trash, IDLE for push and
  CONDSTORE/QRESYNC for incremental sync when available, SMTP for
  sending. Deep links are off by default (optional webmail URL opens
  the mailbox, not the message).

Provider errors are always wrapped with `errdefs` kinds
(auth / rate-limit / network / permanent); the queue's retry policy is
keyed on the kind.

## Storage

Ent over SQLite (WAL, foreign keys, CGO-free driver). Cache only:
metadata plus a truncated plain-text body (default 32 KiB) — no HTML, no
attachments, no blobs. Entities: `Account` (config JSON + sync cursor;
secrets live in the system keyring, keyed by account ID), `Thread`,
`Message`, `PendingOp`, `NotifyRule`. Retention: threads older than N
days (default 30) are pruned unless starred or snoozed.

## Sync, queue, reconciliation

The remote server is the authority; the local DB is a cache plus a queue
of pending operations. Every user action applies locally at once
(optimistic UI) and becomes a `PendingOp`; a per-account executor drains
the queue with exponential backoff (max 5 attempts, then `failed` +
revert + notification). **Reconciliation invariant:** sync overwrites
local thread state (last-writer-wins, remote wins) *except* for threads
holding a pending/inflight op, which stay frozen until the op finishes.

**Snooze is simulated locally**: snooze = archive now + `snoozed_until`;
a persistent scheduler (rebuilt from the DB at startup) re-inboxes the
thread when it wakes. Any remote change to a snoozed thread cancels the
snooze and re-surfaces it. Snoozes are invisible to Gmail's own
"Snoozed" view and need the daemon running to wake.

**Action hooks** (`internal/rules`) declaratively expand one op into
several before enqueueing — preview/reply/trash also mark read (default
on), star can un-archive (default off).

## Notifications

D-Bus `org.freedesktop.Notifications` with inline actions (archive,
mark read, open in web, snooze) when the server supports them;
`notify-send` fallback. `NotifyRule`s per account + label decide sound,
urgency, or silent counter-only updates (default: INBOX notifies,
other monitored labels are silent). DND windows accumulate
notifications and emit a summary on exit. Unread counts are exposed over
IPC for bars/widgets and drawn as a tray badge.

## Surfaces

- **CLI/IPC**: `dmail show|toggle|run|daemon|sync|status|dnd|list|open`,
  unix socket with line-delimited JSON (same protocol shape as dcal).
- **HTTP API** (HUMA + chi, localhost, ephemeral port): read side for
  the QML UI; mutations go through IPC so all surfaces share one path.

## Delivery rings

1. **MVP**: skeleton, Ent schema, Gmail provider, op queue +
   reconciliation, tray + triage popup + preview, basic notifications,
   CLI basics, keyring.
2. IMAP provider, snooze scheduler, NotifyRules + sounds, DND, batch
   actions, action hooks, quick reply + compose, `list --json`.
3. (Not designed yet) Gmail Pub/Sub push, FTS5 search, OTP auto-trash,
   calendar-aware DND via dcal IPC, user rules, AUR packaging.
