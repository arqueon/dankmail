# Local mail search

The triage search field searches literal text in cached subjects, snippets,
senders and plain-text bodies. Searches include archived and snoozed threads;
account, unread and starred filters still apply. Server-side history search and
opening webmail remain separate actions.

The list displays 200 threads at a time and shows a **Load more** button when
another page exists. The displayed count is the number of loaded threads, not
an estimate of the total. `threads.list` keeps its existing array response and
accepts an optional nonnegative `offset`. Pages sort by message date and then
thread ID. They are not a database snapshot: new mail or deletions between page
requests can shift offsets. Refreshing starts from the first page again.

## Index and migration

On the first start of the updated daemon, two SQLite FTS5 trigram indexes are
built from the existing cache in one transaction. This can delay that initial
startup and needs disk space for the index. The indexes use external content,
so they do not store another copy of mail bodies. Triggers maintain them as
mail is inserted, changed, pruned or removed with an account. No remote sync or
account reauthorization is needed to build the indexes.

Indexed LIKE queries produce candidates and a literal substring check rejects
wildcard false positives, preserving searches for `%`, `_` and punctuation.
Queries without a usable three-character sequence fall back to scans. This
uses SQLite's [trigram and external-content FTS5 support](https://www.sqlite.org/fts5.html#the_trigram_tokenizer).

The interface immediately invalidates results when text or filters change,
coalesces refreshes while a request is running, and discards superseded
responses. It updates list rows by thread ID instead of recreating unchanged
delegates. A socket disconnect clears the pending search so reconnection can
start fresh. Restart the interface as well as the daemon after updating both.

## Validation

The test target requires Go, Node.js (22 or later) and Python 3:

```sh
make -C core test
make -C core vet
make -C core build
go -C core test -race ./repo
go -C core test ./repo -run '^$' -bench BenchmarkThreadSearch -benchtime=3x -count=1 -v
```

Repository tests cover backfilling an existing database, reopening it,
insert/update/delete triggers, cascade deletion, rollback, literal search
compatibility, filters and pagination beyond 200 results. JavaScript tests run
the shipped controller and model reconciler with delayed responses,
coalescing, filter changes, pagination, errors and reconnects.

The benchmark uses 10,000 synthetic threads with approximately 4 KiB bodies.
Its legacy-query comparison stops after five seconds per search. Results are
machine- and dataset-dependent; it does not access a real mailbox.

Native UI checks should use an isolated `DMAIL_SOCKET` with synthetic data:
verify the loading and empty states, scroll to **Load more**, load a final
partial page, enter a new query while an earlier response is delayed, and
disconnect/reconnect the socket. Check the QML log and the rendered list;
the JavaScript tests alone do not validate rendering.
