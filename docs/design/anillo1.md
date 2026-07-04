# Anillo 1 — diseño de tipos e interfaces (para validar antes de codificar)

Per §10 of the spec: types first, human review, then code. Two
subsystems carry almost all the logical risk of the MVP; their contracts
are below. Everything else in Anillo 1 (daemon wiring, IPC handlers,
HTTP read endpoints, tray/popup QML) is composition around these.

## 1. Cola de operaciones + reconciliación (`internal/sync`)

```go
// Op is the domain view of a PendingOp row (ent-free so it's testable).
type Op struct {
    ID        int
    AccountID string
    Type      OpType       // mirrors the ent enum
    ThreadIDs []string     // provider-native
    Payload   OpPayload
    Attempts  int
}

// OpPayload carries op-specific data. Exactly one field is set.
type OpPayload struct {
    Reply   *provider.ReplyDraft   `json:"reply,omitempty"`
    Compose *provider.ComposeDraft `json:"compose,omitempty"`
    Snooze  *SnoozePayload         `json:"snooze,omitempty"`
    // Prev holds the pre-op thread state (unread/starred/inInbox per
    // thread) so a failed op can revert the optimistic local change.
    Prev map[string]ThreadState `json:"prev,omitempty"`
}

// Enqueue is the ONLY write path for user actions. It:
//  1. runs rules.Policies.Expand (action hooks) → extra ops,
//  2. snapshots ThreadState into Payload.Prev,
//  3. applies the optimistic local change,
//  4. inserts the PendingOp rows — all in one transaction,
//  5. pokes the account's Executor.
func (q *Queue) Enqueue(ctx context.Context, op Op) error

// Executor: one goroutine per account. Loop:
//   pick ops due (state=pending, next_attempt_at<=now) →
//   coalesce homogeneous adjacent ops (same Type) into one provider
//   call (Gmail batchModify) → state=inflight → call provider →
//   done | retry with backoff | failed.
// Failure policy keyed on errdefs.KindOf:
//   KindAuth      → account status=auth_error, ops stay pending, notify.
//   KindRateLimit → retry, honor Retry-After if the error carries it.
//   KindNetwork   → retry (2s, 8s, 30s, 2m, 5m).
//   KindPermanent → state=failed, revert from Payload.Prev, notify.
//   attempts==5   → same as permanent.
type Executor struct{ /* accountID, repo, provider, notifier, bus */ }

// Reconciler applies provider.Changes to the local cache.
// Invariant (the one rule): a thread with ops pending/inflight is
// FROZEN — sync must not touch its flags/inbox state; message inserts
// are still allowed. Implemented as: frozen = set of thread IDs from
// PendingOp where state IN (pending, inflight).
// Snooze cancellation: any remote change on a snoozed thread clears
// snoozed_until and re-inboxes it (spec §5).
type Reconciler struct{ /* repo */ }
func (r *Reconciler) Apply(ctx context.Context, accountID string, ch provider.Changes) error

// Scheduler wakes snoozed threads. Rebuilt at startup from
// Thread.snoozed_until; ticks every 30s (no timers to persist).
// On wake: enqueue unarchive (+mark_unread if configured) + notify.
type Scheduler struct{ /* repo, queue, notifier */ }
```

**Tests primero** (unit, in-memory repo): optimistic apply + revert on
permanent failure; frozen-thread rule vs concurrent sync; hook
expansion; backoff/attempt transitions; snooze wake + remote-change
cancellation; coalescing batches homogéneos.

## 2. Provider Gmail (`internal/provider/gmail`)

```go
// gmailAPI is the thin seam over the generated client — everything the
// provider touches, and nothing more — so tests use a fake without HTTP.
type gmailAPI interface {
    ListThreads(ctx context.Context, labelIDs []string, pageToken string) (*gmail.ListThreadsResponse, error)
    GetThread(ctx context.Context, id string, format string) (*gmail.Thread, error)
    ListHistory(ctx context.Context, startHistoryID uint64, pageToken string) (*gmail.ListHistoryResponse, error)
    BatchModify(ctx context.Context, msgIDs []string, addLabels, removeLabels []string) error
    ModifyThread(ctx context.Context, threadID string, addLabels, removeLabels []string) error
    SendMessage(ctx context.Context, threadID string, raw []byte) error
    GetProfile(ctx context.Context) (*gmail.Profile, error) // email + historyId
}

// Provider implements provider.Provider.
// Capabilities: ModifyFlags|Archive|Trash|SendReply|Compose|DeepLink|HistorySync.
// (CapPush llega con Pub/Sub en anillo 3.)
//
// Sync:
//   cursor == ""      → threads.list(INBOX + monitored labels), full.
//   cursor == history → history.list; on 404 → FullResync (full path).
//   returns new cursor = profile.historyId (initial) or max historyId seen.
// Label semantics: read=-UNREAD star=+STARRED archive=-INBOX trash=+TRASH.
// Errors: 401/invalid_grant→KindAuth, 403 rateLimitExceeded/429→KindRateLimit,
// 5xx/net→KindNetwork, 400/404 (except history 404)→KindPermanent.
//
// WebLink: prefer https://mail.google.com/mail/u/0/#all/{threadID}?authuser={email};
// fallback #search/rfc822msgid:{Message-ID}. Both implemented (spec §3.2).
```

```go
// internal/mailmime (shared with the future IMAP provider):
// BuildReply constructs the plain-text MIME reply: Subject "Re: ..."
// (idempotent), In-Reply-To + References extended from the original,
// To/Cc according to ReplyAll, quoted-printable UTF-8 body.
func BuildReply(orig provider.MessageDelta, from string, d provider.ReplyDraft) ([]byte, error)
func BuildCompose(from string, d provider.ComposeDraft) ([]byte, error)
```

**Tests primero**: mapeo history.list→Changes (added/deleted/label
changes, dedup por hilo); expiración de cursor; clasificación de
errores; construcción MIME (threading headers, Re: idempotente,
reply-all vs reply, UTF-8).

## Orden de implementación propuesto

1. `internal/mailmime` + tests (puro, sin dependencias).
2. `internal/sync` (Queue/Executor/Reconciler/Scheduler) + tests con
   provider fake y repo en memoria.
3. `internal/provider/gmail` + fake de `gmailAPI` + tests.
4. OAuth broker (loopback) + keyring wiring.
5. Daemon: ensamblar engine + IPC handlers + HTTP reads + notificaciones.
6. QML: tray + popup mínimos contra el API.
