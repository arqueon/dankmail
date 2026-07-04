pragma Singleton
pragma ComponentBehavior: Bound

import QtQuick
import Quickshell
import Quickshell.Io
import qs.Common
import qs.Services

// NOTE: responses may arrive out of order (the daemon dispatches each
// request on its own goroutine so long-running calls like the OAuth
// complete don't block the connection); the pendingRequests map keyed by
// id handles that.

// Bridge to the dmail daemon over its unix IPC socket, mirroring
// dankcalendar's DankCalService: one request/response connection with a
// pending-callback map, one subscription connection for daemon events.
// All reads and mutations go over IPC (the HTTP API stays for scripting).
Singleton {
    id: root

    readonly property var log: Log.scoped("DankMailService")

    readonly property string socketPath: {
        const env = Quickshell.env("DMAIL_SOCKET");
        if (env && env !== "")
            return env;
        const runtime = Quickshell.env("XDG_RUNTIME_DIR");
        return (runtime && runtime !== "" ? runtime : "/tmp") + "/dankmail.sock";
    }

    property bool connected: false
    property bool connecting: false
    property bool subscribed: false
    property int apiVersion: 0
    property var capabilities: []

    property var accounts: []
    property var threads: []
    property var currentThread: null
    property bool threadsLoading: false
    property bool dndEnabled: false

    // Triage filters (drive refreshThreads).
    property bool filterUnread: false
    property bool filterStarred: false
    property string filterAccount: ""
    // searchQuery sweeps the whole local cache (subject/sender/snippet/
    // body); empty = normal inbox view. Full-history search continues in
    // the webmail via openWebSearch.
    property string searchQuery: ""

    readonly property int unreadTotal: {
        let n = 0;
        for (const a of accounts)
            n += a.unread || 0;
        return n;
    }

    readonly property var accountPalette: ["#7287fd", "#f38ba8", "#a6e3a1", "#fab387", "#cba6f7", "#94e2d5", "#f9e2af", "#89dceb"]

    function accountColor(accountId) {
        for (let i = 0; i < accounts.length; i++) {
            if (accounts[i].id === accountId)
                return accountPalette[i % accountPalette.length];
        }
        return accountPalette[0];
    }

    function accountEmail(accountId) {
        for (const a of accounts) {
            if (a.id === accountId)
                return a.email;
        }
        return "";
    }

    // parseAddress splits an RFC-style display string ('"Ada L." <a@x>')
    // into {name, email}; bare addresses land in email with empty name.
    function parseAddress(raw) {
        if (!raw)
            return {
                "name": "",
                "email": ""
            };
        const m = raw.match(/^\s*"?([^"<]*?)"?\s*<([^>]+)>\s*$/);
        if (m)
            return {
                "name": m[1].trim(),
                "email": m[2].trim()
            };
        return {
            "name": "",
            "email": raw.trim()
        };
    }

    // displayName favors the human name, falling back to the address.
    function displayName(raw) {
        const a = parseAddress(raw);
        return a.name !== "" ? a.name : a.email;
    }

    // senderColor: stable per-sender avatar color (Gmail-style), hashed
    // over the display string.
    readonly property var senderPalette: ["#7287fd", "#f38ba8", "#a6e3a1", "#fab387", "#cba6f7", "#94e2d5", "#f9e2af", "#89dceb", "#f2cdcd", "#b4befe"]

    function senderColor(raw) {
        const s = displayName(raw);
        let h = 0;
        for (let i = 0; i < s.length; i++)
            h = (h * 31 + s.charCodeAt(i)) & 0x7fffffff;
        return senderPalette[h % senderPalette.length];
    }

    function senderInitial(raw) {
        const s = displayName(raw);
        return s.length > 0 ? s[0].toUpperCase() : "?";
    }

    // Account wizard data (guides/presets served by the daemon, dcal
    // pattern).
    property var gmailSetupSteps: []
    property string gmailDefaultClientId: ""
    property bool gmailHasDefaultCreds: false
    property var imapPresets: []

    // Daemon settings (notification actions, snooze default, chained
    // policies) — live-updated via settings.set.
    property var settingsData: ({})

    property var pendingRequests: ({})
    property int requestCounter: 0

    signal connectionStateChanged
    signal windowActionRequested(string action)
    signal opFailed(string opType, string error)

    Component.onCompleted: socketProbe.running = true

    // Reprobe while the daemon is down so the UI self-heals.
    Timer {
        id: probeRetry
        interval: 3000
        repeat: false
        onTriggered: socketProbe.running = true
    }

    Process {
        id: socketProbe
        command: ["test", "-S", root.socketPath]
        running: false
        onExited: code => {
            if (code === 0) {
                root.connectSocket();
            } else {
                probeRetry.restart();
            }
        }
    }

    function connectSocket() {
        if (connected || connecting)
            return;
        connecting = true;
        requestSocket.connected = true;
    }

    DankSocket {
        id: requestSocket
        path: root.socketPath
        connected: false

        onConnectionStateChanged: {
            if (connected) {
                root.connected = true;
                root.connecting = false;
                root.connectionStateChanged();
                subscribeSocket.connected = true;
                root.refreshAll();
            } else {
                root.connected = false;
                root.connecting = false;
                root.connectionStateChanged();
                probeRetry.restart();
            }
        }

        parser: SplitParser {
            onRead: line => {
                if (!line || line.length === 0)
                    return;
                let response;
                try {
                    response = JSON.parse(line);
                } catch (e) {
                    root.log.warn("bad response", line.substring(0, 200));
                    return;
                }
                root._handleResponse(response);
            }
        }
    }

    DankSocket {
        id: subscribeSocket
        path: root.socketPath
        connected: false

        onConnectionStateChanged: {
            root.subscribed = connected;
            if (connected)
                subscribeSocket.send({
                    "id": root._nextId(),
                    "method": "subscribe"
                });
        }

        parser: SplitParser {
            onRead: line => {
                if (!line || line.length === 0)
                    return;
                let event;
                try {
                    event = JSON.parse(line);
                } catch (e) {
                    return;
                }
                root._handleEvent(event);
            }
        }
    }

    function _nextId() {
        requestCounter++;
        return requestCounter;
    }

    function _handleResponse(response) {
        // First line of every connection is the capabilities handshake.
        if (response.apiVersion !== undefined) {
            apiVersion = response.apiVersion;
            capabilities = response.features || [];
            return;
        }
        const cb = pendingRequests[response.id];
        if (cb) {
            delete pendingRequests[response.id];
            cb(response);
        }
    }

    function _handleEvent(event) {
        switch (event.topic) {
        case "threads.changed":
        case "unread.changed":
        case "snooze.woke":
            refreshDebounce.restart();
            break;
        case "account.auth":
        case "accounts.changed":
            refreshAccounts();
            refreshDebounce.restart();
            break;
        case "dnd.changed":
            dndEnabled = !!(event.payload && event.payload.enabled);
            break;
        case "settings.changed":
            refreshSettings();
            break;
        case "ui.show":
            windowActionRequested("show");
            break;
        case "ui.toggle":
            windowActionRequested("toggle");
            break;
        case "op.failed":
            {
                const p = event.payload || {};
                opFailed(p.opType || "", p.error || "");
                refreshDebounce.restart();
                break;
            }
        }
    }

    Timer {
        id: refreshDebounce
        interval: 300
        repeat: false
        onTriggered: {
            root.refreshAccounts();
            root.refreshThreads();
            root.reloadCurrentThread();
        }
    }

    function sendRequest(method, params, callback) {
        if (!connected) {
            if (callback)
                callback({
                    "error": "not connected to dmail daemon"
                });
            return;
        }
        const id = _nextId();
        if (callback)
            pendingRequests[id] = callback;
        requestSocket.send({
            "id": id,
            "method": method,
            "params": params || {}
        });
    }

    // ---- reads ---------------------------------------------------------

    function refreshAll() {
        refreshAccounts();
        refreshThreads();
        refreshDnd();
        refreshGmailSetupSteps();
        refreshSettings();
    }

    function refreshSettings() {
        sendRequest("settings.get", null, resp => {
            if (!resp.error && resp.result)
                settingsData = resp.result;
        });
    }

    // updateSettings sends a partial patch; the daemon merges, persists
    // and republishes.
    function updateSettings(patch, callback) {
        sendRequest("settings.set", patch, resp => {
            if (!resp.error && resp.result)
                settingsData = resp.result;
            if (callback)
                callback(resp);
        });
    }

    function refreshAccounts() {
        sendRequest("accounts.list", null, resp => {
            if (resp.error) {
                log.warn("accounts.list:", resp.error);
                return;
            }
            accounts = resp.result || [];
        });
    }

    function refreshThreads() {
        threadsLoading = true;
        const params = {
            "inbox": searchQuery === "",
            "limit": 200
        };
        if (searchQuery !== "")
            params.query = searchQuery;
        if (filterUnread)
            params.unread = true;
        if (filterStarred)
            params.starred = true;
        if (filterAccount !== "")
            params.account = filterAccount;
        sendRequest("threads.list", params, resp => {
            threadsLoading = false;
            if (resp.error) {
                log.warn("threads.list:", resp.error);
                return;
            }
            threads = resp.result || [];
        });
    }

    function loadThread(id, markRead) {
        sendRequest("threads.get", {
            "id": id
        }, resp => {
            if (resp.error) {
                log.warn("threads.get:", resp.error);
                return;
            }
            currentThread = resp.result;
            if (markRead)
                sendRequest("threads.previewOpened", {
                    "id": id
                }, null);
        });
    }

    function reloadCurrentThread() {
        if (currentThread)
            loadThread(currentThread.id, false);
    }

    // ---- triage ops (optimistic on the daemon side) ---------------------

    function _op(method, ids) {
        sendRequest(method, {
            "ids": ids
        }, resp => {
            if (resp.error)
                log.warn(method + ":", resp.error);
            refreshDebounce.restart();
        });
    }

    function markRead(ids) {
        _op("ops.markRead", ids);
    }
    function markUnread(ids) {
        _op("ops.markUnread", ids);
    }
    function star(ids) {
        _op("ops.star", ids);
    }
    function unstar(ids) {
        _op("ops.unstar", ids);
    }
    function archive(ids) {
        _op("ops.archive", ids);
    }
    function trash(ids) {
        _op("ops.trash", ids);
    }

    function snooze(ids, untilDate) {
        sendRequest("ops.snooze", {
            "ids": ids,
            "until": untilDate.toISOString()
        }, resp => {
            if (resp.error)
                log.warn("ops.snooze:", resp.error);
            refreshDebounce.restart();
        });
    }

    function openWeb(id) {
        sendRequest("ui.openLink", {
            "id": id
        }, null);
    }

    // contactsNeedReauth: some account's token predates the contacts
    // scopes; Google suggestions need a re-consent (account reauth).
    property bool contactsNeedReauth: false

    // searchContacts feeds the compose autocomplete (ranked merge of
    // mail correspondents + Google contacts).
    function searchContacts(query, accountId, callback) {
        const params = {
            "query": query
        };
        if (accountId && accountId !== "")
            params.account = accountId;
        sendRequest("contacts.search", params, resp => {
            if (resp.error) {
                log.warn("contacts.search:", resp.error);
                callback([]);
                return;
            }
            contactsNeedReauth = !!resp.result.needsReauth;
            callback(resp.result.contacts || []);
        });
    }

    // compose sends a new plain-text message from the given account.
    function compose(accountId, to, subject, body, callback) {
        sendRequest("ops.compose", {
            "account": accountId,
            "to": to,
            "subject": subject,
            "body": body
        }, resp => {
            if (resp.error)
                log.warn("ops.compose:", resp.error);
            if (callback)
                callback(resp);
        });
    }

    // reply enqueues a plain-text reply on the thread (send_reply op:
    // optimistic, retried, mark-read hook applies). replyAll includes
    // the original To/Cc besides the sender.
    function reply(id, body, replyAll, callback) {
        sendRequest("ops.reply", {
            "id": id,
            "body": body,
            "replyAll": replyAll
        }, resp => {
            if (resp.error)
                log.warn("ops.reply:", resp.error);
            refreshDebounce.restart();
            if (callback)
                callback(resp);
        });
    }

    // searchRemoteHistory sweeps the FULL mailbox history server-side
    // (Gmail q= search) and ingests the results as cache backfill; the
    // refresh then surfaces them in the local search view.
    property bool remoteSearching: false

    function searchRemoteHistory(query) {
        if (remoteSearching)
            return;
        remoteSearching = true;
        const params = {
            "query": query
        };
        if (filterAccount !== "")
            params.account = filterAccount;
        sendRequest("threads.searchRemote", params, resp => {
            remoteSearching = false;
            if (resp.error)
                log.warn("threads.searchRemote:", resp.error);
            refreshThreads();
        });
    }

    // openWebSearch continues the query in the webmail (Gmail #search
    // deep link), scoped to the account filter when one is active.
    function openWebSearch(query) {
        const params = {
            "query": query
        };
        if (filterAccount !== "")
            params.account = filterAccount;
        sendRequest("ui.openSearch", params, resp => {
            if (resp.error)
                log.warn("ui.openSearch:", resp.error);
        });
    }

    function syncNow() {
        sendRequest("system.sync", {}, resp => {
            if (resp.error)
                log.warn("system.sync:", resp.error);
        });
    }

    function refreshDnd() {
        sendRequest("dnd.status", null, resp => {
            if (!resp.error && resp.result)
                dndEnabled = !!resp.result.enabled;
        });
    }

    function setDnd(enabled) {
        sendRequest(enabled ? "dnd.on" : "dnd.off", null, () => refreshDnd());
    }

    function quit() {
        sendRequest("system.exit", null, null);
        Qt.quit();
    }

    // ---- gmail account wizard -------------------------------------------

    function refreshGmailSetupSteps() {
        sendRequest("accounts.gmail.setupGuide", null, resp => {
            if (resp.error || !resp.result)
                return;
            gmailSetupSteps = resp.result.steps || [];
            gmailDefaultClientId = resp.result.defaultClientId || "";
            gmailHasDefaultCreds = !!resp.result.hasDefaultCreds;
        });
        sendRequest("accounts.imap.presets", null, resp => {
            if (!resp.error && resp.result)
                imapPresets = resp.result;
        });
    }

    // params: {email, password, host, port, security, username, smtpHost,
    // smtpPort, webmailUrl}. The daemon tests the connection before
    // storing anything. callback({accountId, email}) or {error}.
    function addImapAccount(params, callback) {
        sendRequest("accounts.imap.add", params, resp => callback(resp.error ? resp : resp.result));
    }

    // callback({state, authUrl}) or callback({error}). Pasting the whole
    // downloaded client_secret_*.json into the ID field also works: it
    // is detected and forwarded for the daemon to parse.
    function startGmailFlow(clientId, clientSecret, callback) {
        const params = clientId.trim().startsWith("{") ? {
            "clientJson": clientId
        } : {
            "clientId": clientId,
            "clientSecret": clientSecret
        };
        sendRequest("accounts.gmail.start", params, resp => callback(resp.error ? resp : resp.result));
    }

    // Long-running: resolves when the user finishes (or the daemon times
    // out after 5 minutes). callback({accountId, email}) or {error}.
    function completeGmailFlow(state, callback) {
        sendRequest("accounts.gmail.complete", {
            "state": state
        }, resp => callback(resp.error ? resp : resp.result));
    }

    function cancelFlow(state) {
        sendRequest("accounts.flow.cancel", {
            "state": state
        }, null);
    }

    function removeAccount(accountId) {
        sendRequest("accounts.remove", {
            "id": accountId
        }, resp => {
            if (resp.error)
                log.warn("accounts.remove:", resp.error);
        });
    }
}
