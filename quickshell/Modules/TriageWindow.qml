import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Common
import qs.Modals
import qs.Services
import qs.Widgets
import "../Common/ThreadSearch.js" as ThreadSearch
import "../Common/BodyFormatter.js" as BodyFormatter

// Main triage window: unified thread list (left) + plain-text preview
// (right). Everything here is a view over the daemon: actions enqueue
// PendingOps and the list refreshes from daemon events.
FloatingWindow {
    id: window

    signal hideRequested

    // Closing via the window manager (e.g. niri's close-window bind)
    // kills the surface without going through our X/Esc paths; mirror
    // that into the loader so the next "show" recreates the window
    // instead of focusing a ghost.
    onVisibleChanged: {
        if (!visible)
            hideRequested();
    }

    title: "Dank Mail"
    implicitWidth: 980
    implicitHeight: 620
    minimumSize: Qt.size(720, 420)
    color: Theme.surface

    property int selectedThreadId: -1
    property int selectedMessageIndex: -1
    property string operationError: ""
    property string copiedNotice: ""
    property double _lastLinkOpenMs: 0

    Timer {
        id: copiedNoticeTimer
        interval: 2200
        repeat: false
        onTriggered: window.copiedNotice = ""
    }

    function copyWithFeedback(value) {
        if (!value || String(value).trim() === "")
            return;
        DankMailService.copyText(value);
        window.copiedNotice = I18n.tr("Copied: %1", "preview header").arg(String(value).trim());
        copiedNoticeTimer.restart();
    }

    function openBodyLink(rawLink) {
        if (!rawLink)
            return;
        const now = Date.now();
        if (now - window._lastLinkOpenMs < 250)
            return;
        const clean = BodyFormatter.unwrapUrl ? BodyFormatter.unwrapUrl(rawLink) : String(rawLink);
        if (!BodyFormatter.safeUrl(clean))
            return;
        window._lastLinkOpenMs = now;
        DankMailService.openExternalUrl(clean);
    }

    Connections {
        target: DankMailService
        function onOpFailed(opType, error) {
            window.operationError = I18n.tr("Could not apply the action: %1", "operation error").arg(error);
        }
    }

    function visibleThreadMessages() {
        const t = DankMailService.currentThread;
        if (!t || !t.messages)
            return [];
        const filtered = DankMailService.filterLabel === "SENT" ? t.messages.filter(m => m.isSent) : t.messages;
        return filtered.length > 0 ? filtered : t.messages;
    }

    function previewMessage() {
        const messages = visibleThreadMessages();
        if (messages.length === 0)
            return null;
        if (selectedMessageIndex >= 0 && selectedMessageIndex < messages.length)
            return messages[selectedMessageIndex];
        return messages[messages.length - 1];
    }

    function allMessageEmails() {
        const msg = previewMessage();
        const t = DankMailService.currentThread;
        const rawList = [];
        if (msg) {
            if (msg.from)
                rawList.push(msg.from);
            for (const r of (msg.to || []))
                rawList.push(r);
            for (const r of (msg.cc || []))
                rawList.push(r);
        }
        if (t && t.participants) {
            for (const p of t.participants)
                rawList.push(p);
        }
        const seen = {};
        const out = [];
        for (const raw of rawList) {
            const parsed = DankMailService.parseAddress(raw);
            const email = (parsed.email || "").trim();
            const key = email.toLowerCase();
            if (email !== "" && !seen[key]) {
                seen[key] = true;
                out.push(email);
            }
        }
        return out;
    }

    function selectThread(t) {
        selectedThreadId = t.id;
        selectedMessageIndex = -1;
        DankMailService.currentDraft = null;
        replyArea.reset();
        forwardArea.reset();
        DankMailService.loadThread(t.id, true);
    }

    function selectedIds() {
        return selectedThreadId >= 0 ? [selectedThreadId] : [];
    }

    // Selections for bulk operations.
    property var checkedIds: []

    function toggleSpamChecked(id) {
        const i = checkedIds.indexOf(id);
        const next = checkedIds.slice();
        if (i === -1)
            next.push(id);
        else
            next.splice(i, 1);
        checkedIds = next;
    }

    // Temporal Undo State (40-second delay for actions)
    property var undoAction: null
    property var temporarilyHiddenIds: []

    Timer {
        id: undoTimer
        interval: 40000 // 40 seconds
        repeat: false
        onTriggered: window.flushUndoAction()
    }

    function queueUndoableAction(type, ids, extra) {
        // 1. Flush any existing undo action
        window.flushUndoAction();

        // 2. Set up new undo action
        let currentT = DankMailService.currentThread;
        window.undoAction = {
            "type": type,
            "ids": ids,
            "extra": extra || null,
            "currentThread": currentT
        };

        // 3. Mark ids as temporarily hidden
        window.temporarilyHiddenIds = ids;

        // 4. Deselect thread if active
        if (currentT && ids.indexOf(currentT.id) !== -1) {
            DankMailService.currentThread = null;
        }

        // 5. Start timer
        undoTimer.restart();
    }

    function flushUndoAction() {
        if (!window.undoAction)
            return;

        const type = window.undoAction.type;
        const ids = window.undoAction.ids;
        const extra = window.undoAction.extra;

        if (type === "archive") {
            DankMailService.archive(ids);
        } else if (type === "trash") {
            DankMailService.trash(ids);
        } else if (type === "unspam") {
            DankMailService.unspam(ids);
        } else if (type === "spam") {
            DankMailService.spam(ids);
        } else if (type === "snooze") {
            DankMailService.snooze(ids, extra);
        }

        window.undoAction = null;
        window.temporarilyHiddenIds = [];
    }

    function triggerUndo() {
        if (!window.undoAction)
            return;

        undoTimer.stop();
        
        // Restore active thread
        if (window.undoAction.currentThread) {
            DankMailService.currentThread = window.undoAction.currentThread;
        }

        window.undoAction = null;
        window.temporarilyHiddenIds = [];
    }

    function snoozeUntil(kind) {
        const now = new Date();
        let d = new Date(now);
        switch (kind) {
        case "hour":
            d.setHours(d.getHours() + 1);
            break;
        case "evening":
            d.setHours(18, 0, 0, 0);
            if (d <= now)
                d.setDate(d.getDate() + 1);
            break;
        case "tomorrow":
            d.setDate(d.getDate() + 1);
            d.setHours(9, 0, 0, 0);
            break;
        case "nextweek":
            d.setDate(d.getDate() + ((8 - d.getDay()) % 7 || 7));
            d.setHours(9, 0, 0, 0);
            break;
        }
        return d;
    }

    function formatSize(bytes) {
        if (!bytes || bytes <= 0)
            return "";
        if (bytes < 1024)
            return bytes + " B";
        if (bytes < 1024 * 1024)
            return (bytes / 1024).toFixed(0) + " KB";
        return (bytes / (1024 * 1024)).toFixed(1) + " MB";
    }

    // threadAttachments aggregates unique attachment metadata across the
    // whole thread (content never leaves the webmail).
    function threadAttachments() {
        const t = DankMailService.currentThread;
        if (!t || !t.messages)
            return [];
        const seen = {};
        const out = [];
        for (const m of t.messages) {
            for (const a of (m.attachments || [])) {
                const key = a.filename + "|" + a.size;
                if (!seen[key]) {
                    seen[key] = true;
                    out.push(a);
                }
            }
        }
        return out;
    }

    function timeLabel(iso) {
        const d = new Date(iso);
        const now = new Date();
        const sameDay = d.toDateString() === now.toDateString();
        if (sameDay)
            return Qt.formatTime(d, "HH:mm");
        if (d.getFullYear() === now.getFullYear())
            return Qt.formatDate(d, "d MMM");
        return Qt.formatDate(d, "dd/MM/yy");
    }

    function formatBody(text) {
        return BodyFormatter.format(text, {
            linkColor: String(Theme.primary),
            quoteColor: String(Theme.surfaceTextMedium),
            linkLabel: I18n.tr("Link", "message body")
        });
    }

    // Entry points for the shell's pending-intent dispatch (ui.showThread
    // / ui.compose events may arrive before this window exists).
    function openThread(threadId) {
        if (DankMailService.filterLabel === "DRAFTS")
            DankMailService.filterLabel = "";
        selectedThreadId = threadId;
        selectedMessageIndex = -1;
        DankMailService.currentDraft = null;
        replyArea.reset();
        forwardArea.reset();
        DankMailService.loadThread(threadId, true);
    }

    function openReply(threadId) {
        openThread(threadId);
        replyArea.visible = true;
        Qt.callLater(() => replyInput.forceActiveFocus());
    }

    function openCompose() {
        composeModal.show();
    }

    function openDraftById(draftId) {
        DankMailService.filterUnread = false;
        DankMailService.filterStarred = false;
        DankMailService.filterArchived = false;
        DankMailService.filterLabel = "DRAFTS";
        const findAndEdit = () => {
            for (let i = 0; i < DankMailService.drafts.length; i++) {
                if (DankMailService.drafts[i].id === draftId) {
                    DankMailService.currentDraft = DankMailService.drafts[i];
                    composeModal.editDraft(DankMailService.drafts[i]);
                    return true;
                }
            }
            return false;
        };
        if (!findAndEdit()) {
            DankMailService.sendRequest("drafts.list", { "remote": true }, resp => {
                if (!resp.error && resp.result) {
                    DankMailService.drafts = resp.result;
                    findAndEdit();
                }
            });
        }
    }

    AccountAddModal {
        id: accountModal
    }

    SettingsModal {
        id: settingsModal
        onAddAccountRequested: accountModal.show()
    }

    ComposeModal {
        id: composeModal
    }

    Shortcut {
        sequence: "Escape"
        onActivated: {
            // First Escape clears an active search; the second hides.
            if (searchField.text !== "")
                searchField.text = "";
            else
                window.hideRequested();
        }
    }

    Shortcut {
        sequence: "Ctrl+R"
        onActivated: DankMailService.syncNow()
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // ---- header ----------------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 56
            color: Theme.surfaceContainer

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.spacingL
                anchors.rightMargin: Theme.spacingM
                spacing: Theme.spacingM

                DankIcon {
                    name: "mail"
                    size: Theme.iconSize
                    color: Theme.primary
                }

                StyledText {
                    text: "Dank Mail"
                    font.pixelSize: Theme.fontSizeLarge
                    font.weight: Font.DemiBold
                }

                Rectangle {
                    visible: DankMailService.unreadTotal > 0
                    width: unreadLabel.implicitWidth + Theme.spacingM
                    height: 22
                    radius: 11
                    color: Theme.primaryContainer

                    StyledText {
                        id: unreadLabel
                        anchors.centerIn: parent
                        text: DankMailService.unreadTotal
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.primary
                    }
                }

                Item {
                    Layout.preferredWidth: Theme.spacingS
                }

                // Account filter dots.
                Row {
                    spacing: Theme.spacingXS
                    visible: DankMailService.accounts.length > 1

                    Repeater {
                        model: DankMailService.accounts

                        delegate: Rectangle {
                            required property var modelData
                            readonly property bool active: DankMailService.filterAccount === modelData.id

                            width: 22
                            height: 22
                            radius: 11
                            color: active ? DankMailService.accountColor(modelData.id) : "transparent"
                            border.width: 2
                            border.color: DankMailService.accountColor(modelData.id)

                            StateLayer {
                                stateColor: Theme.primary
                                cornerRadius: 11
                                onClicked: {
                                    DankMailService.filterAccount = parent.active ? "" : parent.modelData.id;
                                    DankMailService.refreshThreads();
                                }
                            }
                        }
                    }
                }

                Item {
                    Layout.fillWidth: true
                }

                // Spotlight-style search over the local cache; the web
                // button continues the query in the webmail.
                DankTextField {
                    id: searchField
                    objectName: "threadSearchField"
                    Layout.preferredWidth: 220
                    Layout.preferredHeight: 36
                    iconName: "search"
                    placeholderText: I18n.tr("Search mail…", "search")
                    onTextChanged: {
                        DankMailService.searchQuery = text.trim();
                        searchDebounce.restart();
                    }

                    Timer {
                        id: searchDebounce
                        interval: 250
                        repeat: false
                        onTriggered: {
                            DankMailService.refreshThreads();
                        }
                    }

                }

                // Deep history: server-side Gmail search, results ingested
                // into the cache (old mail becomes triageable locally).
                DankActionButton {
                    visible: searchField.text.trim() !== ""
                    enabled: !DankMailService.remoteSearching
                    iconName: DankMailService.remoteSearching ? "hourglass_top" : "manage_search"
                    iconColor: DankMailService.remoteSearching ? Theme.surfaceTextAlpha : Theme.surfaceText
                    onClicked: DankMailService.searchRemoteHistory(searchField.text.trim())
                }

                DankActionButton {
                    visible: searchField.text.trim() !== ""
                    iconName: "travel_explore"
                    onClicked: DankMailService.openWebSearch(searchField.text.trim())
                }

                StyledText {
                    visible: !DankMailService.connected
                    text: I18n.tr("Daemon offline", "header status")
                    color: Theme.error
                    font.pixelSize: Theme.fontSizeSmall
                }

                DankActionButton {
                    iconName: "edit_square"
                    onClicked: composeModal.show()
                }

                DankActionButton {
                    iconName: "person_add"
                    onClicked: accountModal.show()
                }

                DankActionButton {
                    iconName: "settings"
                    onClicked: settingsModal.show()
                }

                DankActionButton {
                    iconName: DankMailService.dndEnabled ? "notifications_off" : "notifications"
                    iconColor: DankMailService.dndEnabled ? Theme.warning : Theme.surfaceText
                    onClicked: DankMailService.setDnd(!DankMailService.dndEnabled)
                }

                DankActionButton {
                    iconName: "sync"
                    onClicked: DankMailService.syncNow()
                }

                DankActionButton {
                    iconName: "close"
                    onClicked: window.hideRequested()
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: Theme.outlineMedium
        }

        StyledRect {
            Layout.fillWidth: true
            implicitHeight: operationErrorRow.implicitHeight + Theme.spacingM * 2
            visible: window.operationError !== ""
            color: Theme.withAlpha(Theme.error, 0.12)
            RowLayout {
                id: operationErrorRow
                anchors.fill: parent
                anchors.margins: Theme.spacingM
                StyledText {
                    Layout.fillWidth: true
                    text: window.operationError
                    textFormat: Text.PlainText
                    wrapMode: Text.Wrap
                    color: Theme.error
                }
                DankActionButton { iconName: "close"; onClicked: window.operationError = "" }
            }
        }

        // Quick mailbox filters.
        Flow {
            Layout.fillWidth: true
            Layout.margins: Theme.spacingS
            spacing: Theme.spacingXS

            Repeater {
                model: [
                    {
                        "key": "all",
                        "label": I18n.tr("All", "filter")
                    },
                    {
                        "key": "unread",
                        "label": I18n.tr("Unread", "filter")
                    },
                    {
                        "key": "starred",
                        "label": I18n.tr("Starred", "filter")
                    },
                    {
                        "key": "archived",
                        "label": I18n.tr("Archived", "filter")
                    },
                    {
                        "key": "sent",
                        "label": I18n.tr("Sent", "filter")
                    },
                    {
                        "key": "drafts",
                        "label": DankMailService.drafts.length > 0 ? (I18n.tr("Drafts", "filter") + " (" + DankMailService.drafts.length + ")") : I18n.tr("Drafts", "filter")
                    },
                    {
                        "key": "spam",
                        "label": I18n.tr("Spam", "filter")
                    }
                ]

                delegate: StyledRect {
                    required property var modelData
                    readonly property bool active: (modelData.key === "unread" && DankMailService.filterUnread) || (modelData.key === "starred" && DankMailService.filterStarred) || (modelData.key === "archived" && DankMailService.filterArchived) || (modelData.key === "spam" && DankMailService.filterLabel === "SPAM") || (modelData.key === "sent" && DankMailService.filterLabel === "SENT") || (modelData.key === "drafts" && DankMailService.filterLabel === "DRAFTS") || (modelData.key === "all" && !DankMailService.filterUnread && !DankMailService.filterStarred && !DankMailService.filterArchived && DankMailService.filterLabel === "")

                    implicitWidth: Math.ceil(filterLabel.contentWidth) + Theme.spacingL
                    implicitHeight: 30
                    width: implicitWidth
                    height: 30
                    radius: 15
                    color: active ? Theme.primaryContainer : "transparent"

                    StyledText {
                        id: filterLabel
                        anchors.centerIn: parent
                        text: parent.modelData.label
                        wrapMode: Text.NoWrap
                        elide: Text.ElideNone
                        font.pixelSize: Theme.fontSizeSmall
                        color: parent.active ? Theme.primary : Theme.surfaceTextMedium
                    }

                    StateLayer {
                        stateColor: Theme.primary
                        onClicked: {
                            DankMailService.filterUnread = parent.modelData.key === "unread";
                            DankMailService.filterStarred = parent.modelData.key === "starred";
                            DankMailService.filterArchived = parent.modelData.key === "archived";
                            DankMailService.filterLabel = parent.modelData.key === "spam" ? "SPAM" : parent.modelData.key === "sent" ? "SENT" : parent.modelData.key === "drafts" ? "DRAFTS" : "";
                            window.checkedIds = [];
                            if (parent.modelData.key === "drafts") {
                                DankMailService.currentThread = null;
                                DankMailService.refreshDrafts(true);
                            } else {
                                DankMailService.currentDraft = null;
                                DankMailService.refreshThreads();
                            }
                        }
                    }
                }
            }

            // Spam review: one click marks everything listed as
            // read, so the folder can be left "reviewed". Only shown when nothing is selected.
            DankActionButton {
                visible: DankMailService.filterLabel === "SPAM" && DankMailService.threads.some(t => t.unread) && window.checkedIds.length === 0
                iconName: "done_all"
                iconColor: Theme.primary
                onClicked: {
                    const ids = DankMailService.threads.filter(t => t.unread).map(t => t.id);
                    if (ids.length)
                        DankMailService.markRead(ids);
                }
            }

            // GENERAL BULK ACTIONS TOOLBAR
            RowLayout {
                visible: window.checkedIds.length > 0
                spacing: Theme.spacingS

                StyledText {
                    text: I18n.tr("%1 selected", "bulk action").arg(window.checkedIds.length)
                    font.pixelSize: Theme.fontSizeSmall
                    font.weight: Font.DemiBold
                    color: Theme.primary
                }

                DankActionButton {
                    buttonSize: 28
                    iconName: "close"
                    iconColor: Theme.primary
                    onClicked: window.checkedIds = []
                }

                // Divider
                Rectangle {
                    width: 1
                    height: 16
                    color: Theme.outline
                }

                // SPAM-specific bulk actions
                Row {
                    spacing: Theme.spacingXS
                    visible: DankMailService.filterLabel === "SPAM"

                    DankActionButton {
                        buttonSize: 28
                        iconName: "drafts"
                        onClicked: {
                            DankMailService.markRead(window.checkedIds);
                            window.checkedIds = [];
                        }
                    }

                    DankButton {
                        buttonHeight: 32
                        horizontalPadding: Theme.spacingM
                        text: I18n.tr("Not spam", "thread action")
                        iconName: "move_to_inbox"
                        onClicked: {
                            window.queueUndoableAction("unspam", window.checkedIds);
                            window.checkedIds = [];
                        }
                    }

                    DankActionButton {
                        buttonSize: 28
                        iconName: "delete_forever"
                        iconColor: Theme.error
                        onClicked: {
                            window.queueUndoableAction("trash", window.checkedIds);
                            window.checkedIds = [];
                        }
                    }
                }

                // General bulk actions for Inbox, Unread, Starred, All
                Row {
                    spacing: Theme.spacingXS
                    visible: DankMailService.filterLabel !== "SPAM"

                    DankActionButton {
                        buttonSize: 28
                        iconName: "archive"
                        onClicked: {
                            window.queueUndoableAction("archive", window.checkedIds);
                            window.checkedIds = [];
                        }
                    }

                    DankActionButton {
                        buttonSize: 28
                        iconName: "delete"
                        iconColor: Theme.error
                        onClicked: {
                            window.queueUndoableAction("trash", window.checkedIds);
                            window.checkedIds = [];
                        }
                    }

                    DankActionButton {
                        buttonSize: 28
                        iconName: "drafts"
                        onClicked: {
                            DankMailService.markRead(window.checkedIds);
                            window.checkedIds = [];
                        }
                    }

                    DankActionButton {
                        buttonSize: 28
                        iconName: "mark_email_unread"
                        onClicked: {
                            DankMailService.markUnread(window.checkedIds);
                            window.checkedIds = [];
                        }
                    }

                    DankActionButton {
                        buttonSize: 28
                        iconName: "star"
                        iconColor: Theme.warning
                        onClicked: {
                            DankMailService.star(window.checkedIds);
                            window.checkedIds = [];
                        }
                    }

                    DankActionButton {
                        buttonSize: 28
                        iconName: "star_outline"
                        onClicked: {
                            DankMailService.unstar(window.checkedIds);
                            window.checkedIds = [];
                        }
                    }
                }
            }
        }


        // ---- content: list + preview ------------------------------------
        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            // Thread list.
            Item {
                Layout.preferredWidth: 400
                Layout.fillHeight: true

                ListModel {
                    id: threadModel
                }

                Connections {
                    target: DankMailService
                    function onThreadsChanged() {
                        ThreadSearch.syncModel(threadModel, DankMailService.threads);
                    }
                }

                Component.onCompleted: ThreadSearch.syncModel(threadModel, DankMailService.threads)

                ListView {
                    id: threadList
                    objectName: "threadList"
                    visible: DankMailService.filterLabel !== "DRAFTS"
                    anchors.fill: parent
                    clip: true
                    model: threadModel
                    reuseItems: true
                    boundsBehavior: Flickable.StopAtBounds
                    spacing: 0

                    footer: Column {
                        width: threadList.width
                        spacing: Theme.spacingS
                        padding: Theme.spacingS

                        StyledText {
                            text: (DankMailService.threadsLoading || DankMailService.archiveLoading)
                                ? I18n.tr("Loading…", "search")
                                : I18n.tr("Showing %1 threads", "search").arg(DankMailService.threads.length)
                            color: Theme.surfaceTextMedium
                            font.pixelSize: Theme.fontSizeSmall
                        }

                        StyledRect {
                            visible: DankMailService.threadsHasMore
                            width: moreLabel.implicitWidth + Theme.spacingXL
                            height: 36
                            radius: 18
                            color: Theme.primaryContainer
                            enabled: !DankMailService.threadsLoading

                            StyledText {
                                id: moreLabel
                                anchors.centerIn: parent
                                text: I18n.tr("Load more", "search")
                                color: Theme.primary
                            }
                            StateLayer {
                                stateColor: Theme.primary
                                cornerRadius: 18
                                onClicked: DankMailService.loadMoreThreads()
                            }
                        }
                        StyledText {
                            width: parent.width - 2 * Theme.spacingS
                            wrapMode: Text.Wrap
                            visible: (DankMailService.filterArchived || DankMailService.filterLabel === "SENT") && (DankMailService.archiveWarning !== "" || DankMailService.archiveUnsupported)
                            text: DankMailService.filterLabel === "SENT"
                                ? (DankMailService.archiveUnsupported ? I18n.tr("This account only supports cached sent mail.", "search") : I18n.tr("Could not load sent mail. Try again later.", "search"))
                                : DankMailService.archiveUnsupported
                                ? I18n.tr("This account only supports cached archived mail.", "search")
                                : I18n.tr("Could not load archived history. Try again later.", "search")
                            color: Theme.surfaceTextMedium
                        }

                        StyledRect {
                            visible: (DankMailService.filterArchived || DankMailService.filterLabel === "SENT") && DankMailService.archiveHasMore
                            width: archiveMoreLabel.implicitWidth + Theme.spacingXL
                            height: 36
                            radius: 18
                            color: Theme.primaryContainer
                            enabled: !DankMailService.archiveLoading
                            StyledText {
                                id: archiveMoreLabel
                                anchors.centerIn: parent
                                text: DankMailService.filterLabel === "SENT"
                                    ? (DankMailService.archiveLoading ? I18n.tr("Loading sent mail…", "search") : DankMailService.archiveWarning !== "" ? I18n.tr("Retry sent mail", "search") : I18n.tr("Load older sent mail", "search"))
                                    : DankMailService.archiveLoading
                                    ? I18n.tr("Loading archived mail…", "search")
                                    : DankMailService.archiveWarning !== ""
                                        ? I18n.tr("Retry archived history", "search")
                                        : I18n.tr("Load older archived mail", "search")
                                color: Theme.primary
                            }
                            StateLayer {
                                stateColor: Theme.primary
                                cornerRadius: 18
                                onClicked: DankMailService.loadArchiveHistory()
                            }
                        }

                    }

                    delegate: Rectangle {
                        id: row
                        required property string rowJson
                        readonly property var modelData: JSON.parse(rowJson)
                        ListView.onPooled: rowActions.snoozing = false

                        readonly property bool selected: modelData.id === window.selectedThreadId
                        readonly property bool isHidden: window.temporarilyHiddenIds.indexOf(modelData.id) !== -1

                        width: threadList.width
                        height: isHidden ? 0 : 76
                        visible: !isHidden
                        clip: true
                        color: selected ? Theme.surfaceSelected : "transparent"

                        Rectangle {
                            anchors.left: parent.left
                            anchors.top: parent.top
                            anchors.bottom: parent.bottom
                            width: 3
                            color: DankMailService.accountColor(row.modelData.accountId)
                        }

                        RowLayout {
                            anchors.fill: parent
                            anchors.leftMargin: Theme.spacingM
                            anchors.rightMargin: Theme.spacingM
                            spacing: Theme.spacingM

                            readonly property string sender: (row.modelData.participants && row.modelData.participants.length > 0) ? row.modelData.participants[0] : ""
                            id: rowContent

                            // Multi-selection: reserve room for the checkbox
                            // overlay (declared after the row's StateLayer
                            // so it actually receives clicks).
                            Item {
                                visible: true
                                width: 24
                                height: 1
                            }

                            // Gmail-style sender avatar: initial over a
                            // stable per-sender color.
                            Rectangle {
                                width: 38
                                height: 38
                                radius: 19
                                color: DankMailService.senderColor(rowContent.sender)

                                Text {
                                    anchors.centerIn: parent
                                    text: DankMailService.senderInitial(rowContent.sender)
                                    color: "#1d2024"
                                    font.family: Theme.fontFamily
                                    font.pixelSize: Theme.fontSizeLarge
                                    font.weight: Font.DemiBold
                                }
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: 2

                                RowLayout {
                                    Layout.fillWidth: true

                                    // Sender: human name only; the
                                    // address lives in the preview.
                                    StyledText {
                                        Layout.fillWidth: true
                                        text: DankMailService.displayName(rowContent.sender)
                                        // Sender names always bold; read
                                        // vs unread is carried by color.
                                        font.weight: Font.Bold
                                        color: row.modelData.unread ? Theme.surfaceText : Theme.surfaceTextMedium
                                        maximumLineCount: 1
                                    }

                                    StyledText {
                                        text: window.timeLabel(row.modelData.lastMessageAt)
                                        font.pixelSize: Theme.fontSizeSmall
                                        font.weight: row.modelData.unread ? Font.Bold : Font.Normal
                                        color: row.modelData.unread ? Theme.surfaceText : Theme.surfaceTextMedium
                                    }
                                }

                                StyledText {
                                    Layout.fillWidth: true
                                    text: row.modelData.subject || I18n.tr("(no subject)", "thread list")
                                    font.pixelSize: Theme.fontSizeSmall
                                    font.weight: row.modelData.unread ? Font.Bold : Font.Normal
                                    color: row.modelData.unread ? Theme.surfaceText : Theme.surfaceTextMedium
                                    maximumLineCount: 1
                                }

                                StyledText {
                                    Layout.fillWidth: true
                                    text: row.modelData.snippet || ""
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: Theme.surfaceTextMedium
                                    maximumLineCount: 1
                                }
                            }

                            DankIcon {
                                visible: row.modelData.hasAttachments === true
                                name: "attach_file"
                                size: Theme.iconSizeSmall
                                color: Theme.surfaceTextMedium
                            }

                            DankIcon {
                                name: "star"
                                filled: row.modelData.starred
                                size: Theme.iconSizeSmall
                                color: row.modelData.starred ? Theme.warning : Theme.surfaceTextAlpha

                                MouseArea {
                                    anchors.fill: parent
                                    anchors.margins: -6
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: row.modelData.starred ? DankMailService.unstar([row.modelData.id]) : DankMailService.star([row.modelData.id])
                                }
                            }
                        }

                        Rectangle {
                            anchors.bottom: parent.bottom
                            anchors.left: parent.left
                            anchors.right: parent.right
                            height: 1
                            color: Theme.outlineLight
                        }

                        StateLayer {
                            stateColor: Theme.primary
                            cornerRadius: 0
                            onClicked: window.selectThread(row.modelData)
                        }

                        // A HoverHandler (not the StateLayer's containsMouse)
                        // drives the overlay: it keeps reporting hovered while
                        // the pointer is over the action buttons, which sit
                        // above the StateLayer and would otherwise steal the
                        // hover and make the overlay flicker.
                        HoverHandler {
                            id: rowHover
                            // Leaving the row cancels an in-progress snooze
                            // pick so the next hover starts on the actions.
                            onHoveredChanged: if (!hovered)
                                rowActions.snoozing = false
                        }

                        // Selection checkbox: square check for batch actions.
                        // Declared after the row's StateLayer so its clicks
                        // aren't swallowed by the row selection.
                        Rectangle {
                            visible: true
                            readonly property bool checked: window.checkedIds.indexOf(row.modelData.id) !== -1
                            anchors.left: parent.left
                            anchors.leftMargin: Theme.spacingM
                            anchors.verticalCenter: parent.verticalCenter
                            width: 20
                            height: 20
                            radius: 4
                            color: checked ? Theme.primary : "transparent"
                            border.width: 2
                            border.color: checked ? Theme.primary : Theme.outlineMedium

                            DankIcon {
                                anchors.centerIn: parent
                                visible: parent.checked
                                name: "check"
                                size: 14
                                color: Theme.surface
                            }

                            MouseArea {
                                // Larger hit target than the visible box.
                                anchors.centerIn: parent
                                width: 32
                                height: 32
                                onClicked: window.toggleSpamChecked(row.modelData.id)
                            }
                        }

                        // Hover overlay: quick triage actions mirroring the
                        // preview action bar, so a thread can be dispatched
                        // without opening it. Declared after the StateLayer so
                        // its buttons capture clicks; the rest of the row still
                        // selects the thread. Snooze swaps the button row for a
                        // compact preset picker in place — a popup would be
                        // clipped by the list's clip:true.
                        Rectangle {
                            id: rowActions
                            anchors.right: parent.right
                            anchors.rightMargin: Theme.spacingS
                            anchors.verticalCenter: parent.verticalCenter
                            visible: rowHover.hovered
                            width: (snoozing ? snoozeRow.implicitWidth : actionRow.implicitWidth) + Theme.spacingS * 2
                            height: 40
                            radius: Theme.cornerRadius
                            color: Theme.surfaceContainerHigh
                            border.width: 1
                            border.color: Theme.outlineLight

                            property bool snoozing: false

                            function clearIfOpen() {
                                if (row.modelData.id === window.selectedThreadId)
                                    DankMailService.currentThread = null;
                            }

                            function doSnooze(key) {
                                window.queueUndoableAction("snooze", [row.modelData.id], window.snoozeUntil(key));
                                snoozing = false;
                            }

                            // Swallow clicks on the overlay chrome so gaps
                            // between buttons don't fall through and select.
                            MouseArea {
                                anchors.fill: parent
                            }

                            RowLayout {
                                id: actionRow
                                anchors.centerIn: parent
                                visible: !rowActions.snoozing
                                spacing: 0

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: "archive"
                                    onClicked: {
                                        window.queueUndoableAction("archive", [row.modelData.id]);
                                    }
                                }

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: "delete"
                                    iconColor: Theme.error
                                    onClicked: {
                                        window.queueUndoableAction("trash", [row.modelData.id]);
                                    }
                                }

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: row.modelData.unread ? "drafts" : "mark_email_unread"
                                    onClicked: row.modelData.unread ? DankMailService.markRead([row.modelData.id]) : DankMailService.markUnread([row.modelData.id])
                                }

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: "star"
                                    iconColor: row.modelData.starred ? Theme.warning : Theme.surfaceText
                                    onClicked: row.modelData.starred ? DankMailService.unstar([row.modelData.id]) : DankMailService.star([row.modelData.id])
                                }

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: "snooze"
                                    onClicked: rowActions.snoozing = true
                                }

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: "open_in_new"
                                    onClicked: DankMailService.openWeb(row.modelData.id)
                                }
                            }

                            // In-place snooze presets (no calendar — that lives
                            // in the preview). Same 40px height, so nothing is
                            // clipped by the list.
                            RowLayout {
                                id: snoozeRow
                                anchors.centerIn: parent
                                visible: rowActions.snoozing
                                spacing: Theme.spacingXS

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: "arrow_back"
                                    onClicked: rowActions.snoozing = false
                                }

                                Repeater {
                                    model: [
                                        {
                                            "key": "hour",
                                            "label": I18n.tr("In 1 hour", "snooze")
                                        },
                                        {
                                            "key": "evening",
                                            "label": I18n.tr("This evening", "snooze")
                                        },
                                        {
                                            "key": "tomorrow",
                                            "label": I18n.tr("Tomorrow", "snooze")
                                        },
                                        {
                                            "key": "nextweek",
                                            "label": I18n.tr("Next week", "snooze")
                                        }
                                    ]

                                    delegate: StyledRect {
                                        id: rowSnoozeChip
                                        required property var modelData
                                        Layout.preferredWidth: rowSnoozeLabel.implicitWidth + Theme.spacingM
                                        Layout.preferredHeight: 28
                                        radius: 14
                                        color: Theme.surfaceContainerHighest

                                        StyledText {
                                            id: rowSnoozeLabel
                                            anchors.centerIn: parent
                                            text: rowSnoozeChip.modelData.label
                                            font.pixelSize: Theme.fontSizeSmall
                                        }

                                        StateLayer {
                                            stateColor: Theme.primary
                                            onClicked: rowActions.doSnooze(rowSnoozeChip.modelData.key)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                // Drafts list (when Drafts filter is active).
                ListView {
                    id: draftList
                    objectName: "draftList"
                    visible: DankMailService.filterLabel === "DRAFTS"
                    anchors.fill: parent
                    clip: true
                    model: DankMailService.drafts
                    boundsBehavior: Flickable.StopAtBounds
                    spacing: 0

                    delegate: Rectangle {
                        id: draftRow
                        required property var modelData
                        readonly property bool selected: DankMailService.currentDraft && DankMailService.currentDraft.id === modelData.id

                        width: draftList.width
                        height: 76
                        color: selected ? Theme.surfaceSelected : "transparent"

                        Rectangle {
                            anchors.left: parent.left
                            anchors.top: parent.top
                            anchors.bottom: parent.bottom
                            width: 3
                            color: DankMailService.accountColor(draftRow.modelData.accountId)
                        }

                        RowLayout {
                            anchors.fill: parent
                            anchors.leftMargin: Theme.spacingM
                            anchors.rightMargin: Theme.spacingM
                            spacing: Theme.spacingM

                            Rectangle {
                                width: 38
                                height: 38
                                radius: 19
                                color: Theme.primaryContainer

                                DankIcon {
                                    anchors.centerIn: parent
                                    name: "edit_note"
                                    size: Theme.iconSizeSmall
                                    color: Theme.primary
                                }
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: 2

                                RowLayout {
                                    Layout.fillWidth: true

                                    StyledText {
                                        Layout.fillWidth: true
                                        text: (draftRow.modelData.to && draftRow.modelData.to.length > 0) ? draftRow.modelData.to.map(a => DankMailService.displayName(a)).join(", ") : I18n.tr("(no recipients)", "drafts")
                                        font.weight: Font.Bold
                                        color: Theme.surfaceText
                                        maximumLineCount: 1
                                    }

                                    StyledText {
                                        text: draftRow.modelData.updatedAt ? window.timeLabel(draftRow.modelData.updatedAt) : ""
                                        font.pixelSize: Theme.fontSizeSmall
                                        color: Theme.surfaceTextMedium
                                    }
                                }

                                StyledText {
                                    Layout.fillWidth: true
                                    text: draftRow.modelData.subject || I18n.tr("(no subject)", "thread list")
                                    font.pixelSize: Theme.fontSizeSmall
                                    font.weight: Font.Medium
                                    color: Theme.surfaceText
                                    maximumLineCount: 1
                                }

                                StyledText {
                                    Layout.fillWidth: true
                                    text: draftRow.modelData.snippet || ""
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: Theme.surfaceTextMedium
                                    maximumLineCount: 1
                                }
                            }

                            DankIcon {
                                visible: draftRow.modelData.attachments && draftRow.modelData.attachments.length > 0
                                name: "attach_file"
                                size: Theme.iconSizeSmall
                                color: Theme.surfaceTextMedium
                            }
                        }

                        Rectangle {
                            anchors.bottom: parent.bottom
                            anchors.left: parent.left
                            anchors.right: parent.right
                            height: 1
                            color: Theme.outlineLight
                        }

                        StateLayer {
                            stateColor: Theme.primary
                            cornerRadius: 0
                            onClicked: {
                                DankMailService.currentThread = null;
                                DankMailService.currentDraft = draftRow.modelData;
                            }
                        }

                        HoverHandler {
                            id: draftRowHover
                        }

                        Rectangle {
                            visible: draftRowHover.hovered
                            anchors.right: parent.right
                            anchors.rightMargin: Theme.spacingS
                            anchors.verticalCenter: parent.verticalCenter
                            width: draftRowActionLayout.implicitWidth + Theme.spacingS * 2
                            height: 36
                            radius: Theme.cornerRadius
                            color: Theme.surfaceContainerHigh
                            border.width: 1
                            border.color: Theme.outlineLight

                            MouseArea {
                                anchors.fill: parent
                            }

                            RowLayout {
                                id: draftRowActionLayout
                                anchors.centerIn: parent
                                spacing: 0

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: "edit"
                                    onClicked: {
                                        DankMailService.currentDraft = draftRow.modelData;
                                        composeModal.editDraft(draftRow.modelData);
                                    }
                                }

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: "delete"
                                    iconColor: Theme.error
                                    onClicked: DankMailService.deleteDraft(draftRow.modelData.id, null)
                                }
                            }
                        }
                    }
                }

                // Empty state.
                ColumnLayout {
                    anchors.centerIn: parent
                    visible: DankMailService.filterLabel === "DRAFTS" ? (DankMailService.drafts.length === 0 && !DankMailService.draftsLoading) : (DankMailService.threads.length === 0 && !DankMailService.threadsLoading && !DankMailService.archiveLoading)
                    spacing: Theme.spacingM

                    DankIcon {
                        Layout.alignment: Qt.AlignHCenter
                        name: DankMailService.connected ? (DankMailService.filterLabel === "DRAFTS" ? "edit_note" : "inbox") : "cloud_off"
                        size: 48
                        color: Theme.surfaceTextAlpha
                    }

                    StyledText {
                        Layout.alignment: Qt.AlignHCenter
                        text: {
                            if (!DankMailService.connected)
                                return I18n.tr("Daemon offline", "empty state");
                            if (DankMailService.accounts.length === 0)
                                return I18n.tr("No accounts yet", "empty state");
                            if (DankMailService.filterLabel === "DRAFTS")
                                return I18n.tr("No saved drafts", "empty state");
                            if (DankMailService.searchQuery !== "")
                                return I18n.tr("No local results — search the full history or the web", "empty state");
                            if (DankMailService.filterLabel === "SENT")
                                return I18n.tr("No sent mail", "empty state");
                            if (DankMailService.filterArchived)
                                return I18n.tr("No archived mail", "empty state");
                            return I18n.tr("Inbox zero", "empty state");
                        }
                        color: Theme.surfaceTextMedium
                    }

                    StyledRect {
                        visible: DankMailService.connected && DankMailService.accounts.length === 0
                        Layout.alignment: Qt.AlignHCenter
                        width: addAccountLabel.implicitWidth + Theme.spacingXL
                        height: 38
                        radius: 19
                        color: Theme.primaryContainer

                        StyledText {
                            id: addAccountLabel
                            anchors.centerIn: parent
                            text: I18n.tr("Add a Gmail account", "empty state")
                            color: Theme.primary
                        }

                        StateLayer {
                            stateColor: Theme.primary
                            onClicked: accountModal.show()
                        }
                    }
                }
            }

            Rectangle {
                Layout.preferredWidth: 1
                Layout.fillHeight: true
                color: Theme.outlineMedium
            }

            // Preview pane.
            Item {
                Layout.fillWidth: true
                Layout.fillHeight: true

                ColumnLayout {
                    anchors.fill: parent
                    anchors.margins: Theme.spacingL
                    spacing: Theme.spacingM
                    visible: DankMailService.currentThread !== null && DankMailService.filterLabel !== "DRAFTS"

                    StyledText {
                        Layout.fillWidth: true
                        text: DankMailService.currentThread ? (DankMailService.currentThread.subject || I18n.tr("(no subject)", "thread list")) : ""
                        font.pixelSize: Theme.fontSizeXLarge
                        font.weight: Font.DemiBold
                        maximumLineCount: 2
                    }

                    // Thread message selector when the conversation has multiple messages.
                    Flickable {
                        Layout.fillWidth: true
                        Layout.preferredHeight: 28
                        visible: window.visibleThreadMessages().length > 1
                        contentWidth: msgStepRowContainer.implicitWidth
                        contentHeight: 28
                        boundsBehavior: Flickable.StopAtBounds
                        clip: true

                        Row {
                            id: msgStepRowContainer
                            height: 28
                            spacing: Theme.spacingXS

                            Repeater {
                                model: window.visibleThreadMessages()

                                delegate: StyledRect {
                                    id: msgStepChip
                                    required property var modelData
                                    required property int index
                                    readonly property int totalCount: window.visibleThreadMessages().length
                                    readonly property bool active: (window.selectedMessageIndex === -1 && index === totalCount - 1) || window.selectedMessageIndex === index

                                    implicitWidth: Math.ceil(msgStepRow.implicitWidth) + Theme.spacingM * 2
                                    width: implicitWidth
                                    height: 26
                                    radius: 13
                                    color: active ? Theme.primaryContainer : Theme.surfaceContainerHigh

                                    Row {
                                        id: msgStepRow
                                        anchors.centerIn: parent
                                        spacing: 4

                                        StyledText {
                                            text: (msgStepChip.index + 1) + ". " + DankMailService.displayName(msgStepChip.modelData.from)
                                            wrapMode: Text.NoWrap
                                            elide: Text.ElideNone
                                            font.pixelSize: Theme.fontSizeSmall
                                            font.weight: msgStepChip.active ? Font.DemiBold : Font.Normal
                                            color: msgStepChip.active ? Theme.primary : Theme.surfaceTextMedium
                                        }

                                        StyledText {
                                            text: "· " + window.timeLabel(msgStepChip.modelData.date)
                                            wrapMode: Text.NoWrap
                                            elide: Text.ElideNone
                                            font.pixelSize: Theme.fontSizeSmall
                                            color: Theme.surfaceTextAlpha
                                        }
                                    }

                                    StateLayer {
                                        stateColor: Theme.primary
                                        onClicked: window.selectedMessageIndex = msgStepChip.index
                                    }
                                }
                            }
                        }
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.spacingS

                        readonly property var lastMsg: window.previewMessage()
                        readonly property var fromAddr: lastMsg ? DankMailService.parseAddress(lastMsg.from) : null
                        id: fromLine

                        Rectangle {
                            Layout.preferredWidth: 10
                            Layout.preferredHeight: 10
                            width: 10
                            height: 10
                            radius: 5
                            color: DankMailService.currentThread ? DankMailService.accountColor(DankMailService.currentThread.accountId) : "transparent"
                        }

                        StyledText {
                            text: fromLine.fromAddr ? (fromLine.fromAddr.name !== "" ? fromLine.fromAddr.name : fromLine.fromAddr.email) : ""
                            font.weight: Font.DemiBold
                            font.pixelSize: Theme.fontSizeMedium
                            wrapMode: Text.NoWrap
                            elide: Text.ElideRight
                            maximumLineCount: 1
                        }

                        StyledRect {
                            visible: fromLine.fromAddr !== null && fromLine.fromAddr.email !== ""
                            implicitWidth: Math.ceil(senderEmailText.contentWidth) + 18 + Theme.spacingM * 2
                            implicitHeight: 24
                            Layout.preferredWidth: implicitWidth
                            Layout.preferredHeight: 24
                            radius: 12
                            color: Theme.surfaceContainerHigh

                            Row {
                                id: senderEmailRow
                                anchors.centerIn: parent
                                spacing: 4

                                StyledText {
                                    id: senderEmailText
                                    text: fromLine.fromAddr ? fromLine.fromAddr.email : ""
                                    wrapMode: Text.NoWrap
                                    elide: Text.ElideNone
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: Theme.primary
                                    anchors.verticalCenter: parent.verticalCenter
                                }

                                DankIcon {
                                    name: "content_copy"
                                    size: 13
                                    color: Theme.surfaceTextMedium
                                    anchors.verticalCenter: parent.verticalCenter
                                }
                            }

                            StateLayer {
                                stateColor: Theme.primary
                                onClicked: {
                                    if (fromLine.fromAddr && fromLine.fromAddr.email !== "")
                                        window.copyWithFeedback(fromLine.fromAddr.email);
                                }
                            }
                        }

                        Item {
                            Layout.fillWidth: true
                        }

                        StyledText {
                            text: fromLine.lastMsg ? Qt.formatDateTime(new Date(fromLine.lastMsg.date), "ddd d MMM, HH:mm") : ""
                            wrapMode: Text.NoWrap
                            elide: Text.ElideNone
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.surfaceTextAlpha
                            maximumLineCount: 1
                        }

                        StyledRect {
                            visible: window.allMessageEmails().length > 1
                            implicitWidth: Math.ceil(copyAllLabel.contentWidth) + 18 + Theme.spacingM * 2
                            implicitHeight: 24
                            Layout.preferredWidth: implicitWidth
                            Layout.preferredHeight: 24
                            radius: 12
                            color: Theme.surfaceContainerHigh

                            Row {
                                id: copyAllRow
                                anchors.centerIn: parent
                                spacing: 4

                                DankIcon {
                                    name: "content_copy"
                                    size: 13
                                    color: Theme.primary
                                    anchors.verticalCenter: parent.verticalCenter
                                }

                                StyledText {
                                    id: copyAllLabel
                                    text: I18n.tr("Copy all emails", "preview header")
                                    wrapMode: Text.NoWrap
                                    elide: Text.ElideNone
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: Theme.primary
                                    anchors.verticalCenter: parent.verticalCenter
                                }
                            }

                            StateLayer {
                                stateColor: Theme.primary
                                onClicked: window.copyWithFeedback(window.allMessageEmails().join(", "))
                            }
                        }
                    }

                    StyledText {
                        visible: window.copiedNotice !== ""
                        Layout.fillWidth: true
                        text: window.copiedNotice
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.primary
                        elide: Text.ElideRight
                        maximumLineCount: 1
                    }

                    // Recipients of the shown message + thread participants, each clickable to copy.
                    DankFlickable {
                        id: rcptScroll
                        Layout.fillWidth: true
                        Layout.preferredHeight: Math.min(rcptLines.implicitHeight, 84)
                        visible: fromLine.lastMsg !== null && (rcptLines.toList.length > 0 || rcptLines.ccList.length > 0 || rcptLines.bccLikely)
                        contentHeight: rcptLines.implicitHeight
                        clip: true

                        Column {
                            id: rcptLines
                            width: rcptScroll.width
                            spacing: 4

                            readonly property var toList: fromLine.lastMsg ? (fromLine.lastMsg.to || []) : []
                            readonly property var ccList: fromLine.lastMsg ? (fromLine.lastMsg.cc || []) : []
                            readonly property bool bccLikely: {
                                if (!fromLine.lastMsg || fromLine.lastMsg.isSent)
                                    return false;
                                return toList.length === 0 && ccList.length === 0;
                            }

                            Flow {
                                width: parent.width
                                spacing: 4
                                visible: rcptLines.toList.length > 0

                                StyledText {
                                    height: 22
                                    verticalAlignment: Text.AlignVCenter
                                    text: I18n.tr("To", "preview header") + ":"
                                    font.pixelSize: Theme.fontSizeSmall
                                    font.weight: Font.Medium
                                    color: Theme.surfaceTextMedium
                                }

                                Repeater {
                                    model: rcptLines.toList

                                    delegate: StyledRect {
                                        id: toAddrChip
                                        required property string modelData
                                        readonly property var parsed: DankMailService.parseAddress(modelData)
                                        readonly property real maxChipW: Math.max(180, rcptLines.width - 48)
                                        implicitWidth: Math.min(Math.ceil(toAddrLabel.contentWidth) + 18 + Theme.spacingM * 2, maxChipW)
                                        width: implicitWidth
                                        height: 22
                                        radius: 11
                                        color: Theme.surfaceContainerHigh
                                        clip: true

                                        Row {
                                            anchors.centerIn: parent
                                            spacing: 4

                                            StyledText {
                                                id: toAddrLabel
                                                text: DankMailService.formatAddressFull(toAddrChip.modelData)
                                                wrapMode: Text.NoWrap
                                                elide: Text.ElideNone
                                                font.pixelSize: Theme.fontSizeSmall
                                                color: Theme.surfaceText
                                                anchors.verticalCenter: parent.verticalCenter
                                            }

                                            DankIcon {
                                                name: "content_copy"
                                                size: 12
                                                color: Theme.surfaceTextAlpha
                                                anchors.verticalCenter: parent.verticalCenter
                                            }
                                        }

                                        StateLayer {
                                            stateColor: Theme.primary
                                            onClicked: window.copyWithFeedback(toAddrChip.parsed.email !== "" ? toAddrChip.parsed.email : toAddrChip.modelData)
                                        }
                                    }
                                }
                            }

                            Flow {
                                width: parent.width
                                spacing: 4
                                visible: rcptLines.ccList.length > 0

                                StyledText {
                                    height: 22
                                    verticalAlignment: Text.AlignVCenter
                                    text: I18n.tr("Cc", "preview header") + ":"
                                    wrapMode: Text.NoWrap
                                    font.pixelSize: Theme.fontSizeSmall
                                    font.weight: Font.Medium
                                    color: Theme.surfaceTextMedium
                                }

                                Repeater {
                                    model: rcptLines.ccList

                                    delegate: StyledRect {
                                        id: ccAddrChip
                                        required property string modelData
                                        readonly property var parsed: DankMailService.parseAddress(modelData)
                                        readonly property real maxChipW: Math.max(180, rcptLines.width - 48)
                                        implicitWidth: Math.min(Math.ceil(ccAddrLabel.contentWidth) + 18 + Theme.spacingM * 2, maxChipW)
                                        width: implicitWidth
                                        height: 22
                                        radius: 11
                                        color: Theme.surfaceContainerHigh
                                        clip: true

                                        Row {
                                            anchors.centerIn: parent
                                            spacing: 4

                                            StyledText {
                                                id: ccAddrLabel
                                                text: DankMailService.formatAddressFull(ccAddrChip.modelData)
                                                wrapMode: Text.NoWrap
                                                elide: Text.ElideNone
                                                font.pixelSize: Theme.fontSizeSmall
                                                color: Theme.surfaceText
                                                anchors.verticalCenter: parent.verticalCenter
                                            }

                                            DankIcon {
                                                name: "content_copy"
                                                size: 12
                                                color: Theme.surfaceTextAlpha
                                                anchors.verticalCenter: parent.verticalCenter
                                            }
                                        }

                                        StateLayer {
                                            stateColor: Theme.primary
                                            onClicked: window.copyWithFeedback(ccAddrChip.parsed.email !== "" ? ccAddrChip.parsed.email : ccAddrChip.modelData)
                                        }
                                    }
                                }
                            }

                            StyledText {
                                width: parent.width
                                visible: rcptLines.bccLikely
                                text: I18n.tr("Received via BCC — your address is not in To or Cc", "preview header")
                                font.pixelSize: Theme.fontSizeSmall
                                color: Theme.warning
                                wrapMode: Text.WrapAnywhere
                            }
                        }
                    }

                    // Attachment chips: metadata only; clicking opens the
                    // thread in the webmail (content stays there, spec §1).
                    // Capped at two chip rows — beyond that the area scrolls
                    // instead of eating the body's space.
                    DankFlickable {
                        Layout.fillWidth: true
                        Layout.preferredHeight: Math.min(attachFlow.implicitHeight, 2 * 30 + Theme.spacingS)
                        visible: window.threadAttachments().length > 0
                        contentHeight: attachFlow.implicitHeight
                        clip: true

                        Flow {
                            id: attachFlow
                            width: parent.width
                            spacing: Theme.spacingS

                            Repeater {
                                model: window.threadAttachments()

                                delegate: StyledRect {
                                    id: attachChip
                                    required property var modelData
                                    width: attachRow.implicitWidth + Theme.spacingL
                                    height: 30
                                    radius: 15
                                    color: Theme.surfaceContainerHigh
                                    border.width: 1
                                    border.color: Theme.outlineMedium

                                    RowLayout {
                                        id: attachRow
                                        anchors.centerIn: parent
                                        spacing: Theme.spacingXS

                                        DankIcon {
                                            name: attachChip.modelData.mimeType && attachChip.modelData.mimeType.startsWith("image/") ? "image" : "description"
                                            size: Theme.iconSizeSmall
                                            color: Theme.primary
                                        }

                                        StyledText {
                                            text: attachChip.modelData.filename + (window.formatSize(attachChip.modelData.size) !== "" ? "  ·  " + window.formatSize(attachChip.modelData.size) : "")
                                            font.pixelSize: Theme.fontSizeSmall
                                            maximumLineCount: 1
                                        }
                                    }

                                    StateLayer {
                                        stateColor: Theme.primary
                                        onClicked: {
                                            const t = DankMailService.currentThread;
                                            if (t)
                                                DankMailService.openWeb(t.id);
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // Wrap actions at narrow widths, including the labeled rescue action.
                    Flow {
                        Layout.fillWidth: true
                        spacing: Theme.spacingXS

                        DankButton {
                            buttonHeight: 32
                            horizontalPadding: Theme.spacingM
                            text: I18n.tr("Not spam", "thread action")
                            iconName: "move_to_inbox"
                            visible: !!DankMailService.currentThread && (DankMailService.currentThread.labels || []).indexOf("SPAM") !== -1
                            onClicked: {
                                const t = DankMailService.currentThread;
                                if (t)
                                    window.queueUndoableAction("unspam", [t.id]);
                            }
                        }

                        DankActionButton {
                            iconName: "archive"
                            onClicked: {
                                const ids = window.selectedIds();
                                if (ids.length) {
                                    window.queueUndoableAction("archive", ids);
                                }
                            }
                        }

                        DankActionButton {
                            iconName: "delete"
                            iconColor: Theme.error
                            onClicked: {
                                const ids = window.selectedIds();
                                if (ids.length) {
                                    window.queueUndoableAction("trash", ids);
                                }
                            }
                        }

                        DankActionButton {
                            iconName: "report"
                            iconColor: Theme.warning
                            visible: DankMailService.currentThread && (!DankMailService.currentThread.labels || DankMailService.currentThread.labels.indexOf("SPAM") === -1)
                            onClicked: {
                                const ids = window.selectedIds();
                                if (ids.length) {
                                    window.queueUndoableAction("spam", ids);
                                }
                            }
                        }

                        DankActionButton {
                            iconName: DankMailService.currentThread && DankMailService.currentThread.unread ? "drafts" : "mark_email_unread"
                            onClicked: {
                                const t = DankMailService.currentThread;
                                if (!t)
                                    return;
                                t.unread ? DankMailService.markRead([t.id]) : DankMailService.markUnread([t.id]);
                            }
                        }

                        DankActionButton {
                            iconName: "star"
                            iconColor: DankMailService.currentThread && DankMailService.currentThread.starred ? Theme.warning : Theme.surfaceText
                            onClicked: {
                                const t = DankMailService.currentThread;
                                if (!t)
                                    return;
                                t.starred ? DankMailService.unstar([t.id]) : DankMailService.star([t.id]);
                            }
                        }

                        DankActionButton {
                            iconName: "snooze"
                            onClicked: snoozeMenu.visible = !snoozeMenu.visible
                        }

                        DankActionButton {
                            iconName: "reply"
                            iconColor: replyArea.visible ? Theme.primary : Theme.surfaceText
                            onClicked: {
                                forwardArea.reset();
                                replyArea.visible = !replyArea.visible;
                                if (replyArea.visible)
                                    replyInput.forceActiveFocus();
                            }
                        }

                        DankActionButton {
                            iconName: "forward"
                            iconColor: forwardArea.visible ? Theme.primary : Theme.surfaceText
                            onClicked: {
                                replyArea.reset();
                                forwardArea.visible = !forwardArea.visible;
                                if (forwardArea.visible)
                                    forwardToInput.forceActiveFocus();
                            }
                        }

                        DankActionButton {
                            iconName: "open_in_new"
                            onClicked: {
                                const t = DankMailService.currentThread;
                                if (t)
                                    DankMailService.openWeb(t.id);
                            }
                        }
                    }

                    // Snooze options (inline popup) with a calendar picker
                    // for arbitrary dates.
                    StyledRect {
                        id: snoozeMenu
                        visible: false
                        Layout.fillWidth: true
                        implicitHeight: snoozeColumn.implicitHeight + Theme.spacingL
                        color: Theme.surfaceContainerHigh

                        property bool showCustom: false
                        property date customDate: new Date()
                        property int customMinutes: 540 // 09:00

                        onVisibleChanged: {
                            if (visible) {
                                showCustom = false;
                                const d = new Date();
                                d.setDate(d.getDate() + 1);
                                customDate = d;
                                customMinutes = 540;
                            }
                        }

                        function customUntil() {
                            const d = new Date(customDate);
                            d.setHours(Math.floor(customMinutes / 60), customMinutes % 60, 0, 0);
                            return d;
                        }

                        function doSnooze(until) {
                            const ids = window.selectedIds();
                            if (ids.length) {
                                window.queueUndoableAction("snooze", ids, until);
                            }
                            snoozeMenu.visible = false;
                        }

                        ColumnLayout {
                            id: snoozeColumn
                            anchors.centerIn: parent
                            width: parent.width - Theme.spacingL
                            spacing: Theme.spacingS

                            Flow {
                                Layout.fillWidth: true
                                spacing: Theme.spacingS

                                Repeater {
                                    model: [
                                        {
                                            "key": "hour",
                                            "label": I18n.tr("In 1 hour", "snooze")
                                        },
                                        {
                                            "key": "evening",
                                            "label": I18n.tr("This evening", "snooze")
                                        },
                                        {
                                            "key": "tomorrow",
                                            "label": I18n.tr("Tomorrow", "snooze")
                                        },
                                        {
                                            "key": "nextweek",
                                            "label": I18n.tr("Next week", "snooze")
                                        }
                                    ]

                                    delegate: StyledRect {
                                        id: presetChip
                                        required property var modelData
                                        width: snoozeLabel.implicitWidth + Theme.spacingL
                                        height: 30
                                        radius: 15
                                        color: Theme.surfaceContainerHighest

                                        StyledText {
                                            id: snoozeLabel
                                            anchors.centerIn: parent
                                            text: presetChip.modelData.label
                                            font.pixelSize: Theme.fontSizeSmall
                                        }

                                        StateLayer {
                                            stateColor: Theme.primary
                                            onClicked: snoozeMenu.doSnooze(window.snoozeUntil(presetChip.modelData.key))
                                        }
                                    }
                                }

                                StyledRect {
                                    width: customChipLabel.implicitWidth + Theme.spacingL
                                    height: 30
                                    radius: 15
                                    color: snoozeMenu.showCustom ? Theme.primaryContainer : Theme.surfaceContainerHighest

                                    StyledText {
                                        id: customChipLabel
                                        anchors.centerIn: parent
                                        text: I18n.tr("Pick date…", "snooze")
                                        font.pixelSize: Theme.fontSizeSmall
                                        color: snoozeMenu.showCustom ? Theme.primary : Theme.surfaceText
                                    }

                                    StateLayer {
                                        stateColor: Theme.primary
                                        onClicked: snoozeMenu.showCustom = !snoozeMenu.showCustom
                                    }
                                }
                            }

                            // The pickers are width-less Items by design
                            // (dcal pattern): they collapse unless the
                            // parent sizes them explicitly. Flow wraps
                            // them on narrow panes instead of clipping.
                            Flow {
                                visible: snoozeMenu.showCustom
                                Layout.fillWidth: true
                                spacing: Theme.spacingS

                                DankDatePicker {
                                    width: 210
                                    height: 40
                                    selectedDate: snoozeMenu.customDate
                                    onDateSelected: value => snoozeMenu.customDate = value
                                }

                                DankTimePicker {
                                    width: 140
                                    height: 40
                                    minutes: snoozeMenu.customMinutes
                                    use24Hour: true
                                    onTimeSelected: value => snoozeMenu.customMinutes = value
                                }

                                StyledRect {
                                    readonly property bool ready: snoozeMenu.customUntil() > new Date()
                                    width: confirmLabel.implicitWidth + Theme.spacingXL
                                    height: 40
                                    radius: 20
                                    color: ready ? Theme.primaryContainer : Theme.surfaceContainerHighest
                                    opacity: ready ? 1 : 0.6

                                    StyledText {
                                        id: confirmLabel
                                        anchors.centerIn: parent
                                        text: I18n.tr("Snooze", "snooze")
                                        font.pixelSize: Theme.fontSizeSmall
                                        color: parent.ready ? Theme.primary : Theme.surfaceTextMedium
                                    }

                                    StateLayer {
                                        disabled: !parent.ready
                                        stateColor: Theme.primary
                                        onClicked: snoozeMenu.doSnooze(snoozeMenu.customUntil())
                                    }
                                }
                            }
                        }
                    }

                    Rectangle {
                        Layout.fillWidth: true
                        Layout.preferredHeight: 1
                        color: Theme.outlineMedium
                    }

                    // Rich formatted body of the selected message.
                    DankFlickable {
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        contentHeight: bodyText.implicitHeight + Theme.spacingL * 2
                        clip: true

                        TextEdit {
                            id: bodyText
                            width: Math.min(parent.width - Theme.spacingL * 2, 760)
                            x: (parent.width - width) / 2
                            y: Theme.spacingL
                            readOnly: true
                            selectByMouse: true
                            wrapMode: TextEdit.Wrap
                            textFormat: TextEdit.RichText
                            color: Theme.surfaceText
                            selectionColor: Theme.primarySelected
                            font.family: Theme.fontFamily
                            font.pixelSize: Theme.fontSizeMedium
                            text: {
                                const message = window.previewMessage();
                                return message ? window.formatBody(message.bodyText || "") : "";
                            }
                            onLinkActivated: link => window.openBodyLink(link)

                            HoverHandler {
                                id: bodyHoverHandler
                                cursorShape: bodyText.hoveredLink !== "" ? Qt.PointingHandCursor : Qt.IBeamCursor
                            }

                            TapHandler {
                                onTapped: eventPoint => {
                                    if (bodyText.selectedText !== "")
                                        return;
                                    const target = bodyText.linkAt(eventPoint.position.x, eventPoint.position.y) || bodyText.hoveredLink;
                                    if (target && target !== "")
                                        window.openBodyLink(target);
                                }
                            }
                        }
                    }

                    // Quick reply: supports attachments, inline links, saving as draft, or expanding to full composer.
                    StyledRect {
                        id: replyArea
                        visible: false
                        Layout.fillWidth: true
                        implicitHeight: replyColumn.implicitHeight + Theme.spacingL
                        color: Theme.surfaceContainerHigh

                        property bool sending: false
                        property bool savingDraft: false
                        property bool replyAll: false
                        property var attachments: []
                        property string draftId: ""
                        property string error: ""

                        function reset() {
                            visible = false;
                            sending = false;
                            savingDraft = false;
                            replyAll = false;
                            attachments = [];
                            draftId = "";
                            error = "";
                            replyInput.text = "";
                        }

                        function addAttachmentFiles(fileUrls) {
                            const list = attachments.slice();
                            for (let i = 0; i < fileUrls.length; i++) {
                                let p = String(fileUrls[i] || "").trim();
                                if (p.startsWith("file://")) {
                                    try {
                                        p = decodeURIComponent(p.substring(7));
                                    } catch (e) {
                                        p = p.substring(7);
                                    }
                                }
                                if (p === "")
                                    continue;
                                const slash = p.lastIndexOf("/");
                                const name = slash >= 0 ? p.substring(slash + 1) : p;
                                list.push({ "filename": name, "path": p, "size": 0, "mimeType": "" });
                            }
                            attachments = list;
                        }

                        function saveAsDraft() {
                            const t = DankMailService.currentThread;
                            if (!t || savingDraft || (replyInput.text.trim() === "" && attachments.length === 0))
                                return;
                            savingDraft = true;
                            error = "";
                            const last = window.previewMessage();
                            const toList = last ? [DankMailService.parseAddress(last.from).email || last.from] : [];
                            const ccList = (replyAll && last) ? (last.to || []).concat(last.cc || []) : [];
                            let subj = t.subject || "";
                            if (!/^re:/i.test(subj.trim()))
                                subj = "Re: " + subj;
                            DankMailService.saveDraft({
                                "id": draftId,
                                "account": t.accountId,
                                "threadId": t.id,
                                "providerThreadId": t.providerThreadId,
                                "inReplyTo": last ? last.providerMessageId : "",
                                "to": toList,
                                "cc": ccList,
                                "subject": subj,
                                "body": replyInput.text,
                                "attachments": attachments
                            }, resp => {
                                replyArea.savingDraft = false;
                                if (resp.error) {
                                    replyArea.error = resp.error;
                                    return;
                                }
                                if (resp.result && resp.result.id)
                                    replyArea.draftId = resp.result.id;
                                window.copiedNotice = I18n.tr("Draft saved", "compose");
                                copiedNoticeTimer.restart();
                            });
                        }

                        function expandToComposer() {
                            const t = DankMailService.currentThread;
                            if (!t)
                                return;
                            const last = window.previewMessage();
                            const toList = last ? [DankMailService.parseAddress(last.from).email || last.from] : [];
                            const ccList = (replyAll && last) ? (last.to || []).map(a => DankMailService.parseAddress(a).email || a) : [];
                            let subj = t.subject || "";
                            if (!/^re:/i.test(subj.trim()))
                                subj = "Re: " + subj;
                            composeModal.editDraft({
                                "id": draftId,
                                "accountId": t.accountId,
                                "threadId": t.id,
                                "providerThreadId": t.providerThreadId,
                                "inReplyTo": last ? last.providerMessageId : "",
                                "to": toList,
                                "cc": ccList,
                                "subject": subj,
                                "body": replyInput.text,
                                "attachments": attachments
                            });
                            replyArea.reset();
                        }

                        function send() {
                            const t = DankMailService.currentThread;
                            if (!t || (replyInput.text.trim() === "" && attachments.length === 0) || sending)
                                return;
                            sending = true;
                            error = "";
                            DankMailService.reply(t.id, replyInput.text, replyAll, resp => {
                                replyArea.sending = false;
                                if (resp.error) {
                                    replyArea.error = resp.error;
                                    return;
                                }
                                replyArea.reset();
                            }, {
                                "attachments": attachments,
                                "draftId": draftId
                            });
                        }

                        FilePickerModal {
                            id: replyFilePicker
                            onFilesSelected: paths => replyArea.addAttachmentFiles(paths)
                        }

                        ColumnLayout {
                            id: replyColumn
                            anchors.left: parent.left
                            anchors.right: parent.right
                            anchors.top: parent.top
                            anchors.margins: Theme.spacingM
                            spacing: Theme.spacingS

                            RowLayout {
                                Layout.fillWidth: true
                                spacing: Theme.spacingS

                                StyledText {
                                    Layout.fillWidth: true
                                    text: {
                                        const t = DankMailService.currentThread;
                                        if (!t || !t.messages || t.messages.length === 0)
                                            return "";
                                        if (replyArea.replyAll)
                                            return I18n.tr("To everyone on the thread", "quick reply");
                                        return I18n.tr("To", "quick reply") + " " + DankMailService.displayName(t.messages[t.messages.length - 1].from);
                                    }
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: Theme.surfaceTextMedium
                                    maximumLineCount: 1
                                }

                                DankActionButton {
                                    buttonSize: 28
                                    iconSize: 16
                                    iconName: "attach_file"
                                    onClicked: replyFilePicker.open()
                                }

                                DankActionButton {
                                    buttonSize: 28
                                    iconSize: 16
                                    iconName: "open_in_full"
                                    onClicked: replyArea.expandToComposer()
                                }

                                StyledRect {
                                    width: replyAllLabel.implicitWidth + Theme.spacingL
                                    height: 28
                                    radius: 14
                                    color: replyArea.replyAll ? Theme.primaryContainer : Theme.surfaceContainerHighest

                                    StyledText {
                                        id: replyAllLabel
                                        anchors.centerIn: parent
                                        text: I18n.tr("Reply all", "quick reply")
                                        font.pixelSize: Theme.fontSizeSmall
                                        color: replyArea.replyAll ? Theme.primary : Theme.surfaceTextMedium
                                    }

                                    StateLayer {
                                        stateColor: Theme.primary
                                        onClicked: replyArea.replyAll = !replyArea.replyAll
                                    }
                                }
                            }

                            Flow {
                                Layout.fillWidth: true
                                spacing: Theme.spacingXS
                                visible: replyArea.attachments.length > 0

                                Repeater {
                                    model: replyArea.attachments

                                    delegate: StyledRect {
                                        id: replyAttChip
                                        required property var modelData
                                        required property int index
                                        width: replyAttRow.implicitWidth + Theme.spacingM
                                        height: 24
                                        radius: 12
                                        color: Theme.surfaceContainerHighest

                                        RowLayout {
                                            id: replyAttRow
                                            anchors.centerIn: parent
                                            spacing: 4

                                            DankIcon {
                                                name: "attach_file"
                                                size: 13
                                                color: Theme.primary
                                            }

                                            StyledText {
                                                text: replyAttChip.modelData.filename
                                                font.pixelSize: Theme.fontSizeSmall
                                            }

                                            DankIcon {
                                                name: "close"
                                                size: 13
                                                color: Theme.surfaceTextMedium

                                                MouseArea {
                                                    anchors.fill: parent
                                                    anchors.margins: -4
                                                    onClicked: {
                                                        const next = replyArea.attachments.slice();
                                                        next.splice(replyAttChip.index, 1);
                                                        replyArea.attachments = next;
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                            }

                            StyledRect {
                                Layout.fillWidth: true
                                implicitHeight: 96
                                radius: Theme.cornerRadiusSmall
                                color: Theme.surfaceContainer
                                border.width: 1
                                border.color: replyInput.activeFocus ? Theme.primary : Theme.outlineLight

                                DankFlickable {
                                    anchors.fill: parent
                                    anchors.margins: Theme.spacingS
                                    contentHeight: replyInput.implicitHeight
                                    clip: true

                                    TextEdit {
                                        id: replyInput
                                        width: parent.width
                                        wrapMode: TextEdit.Wrap
                                        textFormat: TextEdit.PlainText
                                        color: Theme.surfaceText
                                        selectionColor: Theme.primarySelected
                                        font.family: Theme.fontFamily
                                        font.pixelSize: Theme.fontSizeMedium

                                        Keys.onPressed: event => {
                                            if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) {
                                                event.accepted = true;
                                                replyArea.send();
                                            }
                                        }

                                        StyledText {
                                            visible: replyInput.text === "" && !replyInput.activeFocus
                                            text: I18n.tr("Write your reply… Ctrl+Enter sends", "quick reply")
                                            color: Theme.surfaceTextAlpha
                                        }
                                    }
                                }
                            }

                            RowLayout {
                                Layout.fillWidth: true
                                spacing: Theme.spacingS

                                StyledText {
                                    Layout.fillWidth: true
                                    text: replyArea.error
                                    visible: replyArea.error !== ""
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: Theme.error
                                    wrapMode: Text.WordWrap
                                }

                                Item {
                                    Layout.fillWidth: replyArea.error === ""
                                }

                                StyledRect {
                                    readonly property bool canSave: (replyInput.text.trim() !== "" || replyArea.attachments.length > 0) && !replyArea.savingDraft
                                    width: replySaveDraftLabel.implicitWidth + Theme.spacingL
                                    height: 32
                                    radius: 16
                                    color: Theme.surfaceContainerHighest
                                    opacity: canSave ? 1 : 0.6

                                    StyledText {
                                        id: replySaveDraftLabel
                                        anchors.centerIn: parent
                                        text: replyArea.savingDraft ? I18n.tr("Saving…", "compose") : I18n.tr("Save draft", "compose")
                                        font.pixelSize: Theme.fontSizeSmall
                                        color: parent.canSave ? Theme.surfaceText : Theme.surfaceTextMedium
                                    }

                                    StateLayer {
                                        disabled: !parent.canSave
                                        stateColor: Theme.primary
                                        onClicked: replyArea.saveAsDraft()
                                    }
                                }

                                StyledRect {
                                    readonly property bool ready: (replyInput.text.trim() !== "" || replyArea.attachments.length > 0) && !replyArea.sending
                                    width: sendLabel.implicitWidth + Theme.spacingXL
                                    height: 32
                                    radius: 16
                                    color: ready ? Theme.primaryContainer : Theme.surfaceContainerHighest
                                    opacity: ready ? 1 : 0.6

                                    StyledText {
                                        id: sendLabel
                                        anchors.centerIn: parent
                                        text: replyArea.sending ? I18n.tr("Sending…", "quick reply") : I18n.tr("Send", "quick reply")
                                        font.pixelSize: Theme.fontSizeSmall
                                        color: parent.ready ? Theme.primary : Theme.surfaceTextMedium
                                    }

                                    StateLayer {
                                        disabled: !parent.ready
                                        stateColor: Theme.primary
                                        onClicked: replyArea.send()
                                    }
                                }
                            }
                        }
                    }

                    // Quick forward: send the latest message to new
                    // recipients with an optional note. Plain text, no
                    // attachments — same triage-not-management stance as reply.
                    StyledRect {
                        id: forwardArea
                        visible: false
                        Layout.fillWidth: true
                        implicitHeight: forwardColumn.implicitHeight + Theme.spacingL
                        color: Theme.surfaceContainerHigh

                        property bool sending: false
                        property string error: ""
                        property var suggestions: []

                        function reset() {
                            visible = false;
                            sending = false;
                            error = "";
                            suggestions = [];
                            forwardToInput.text = "";
                            forwardNote.text = "";
                        }

                        function addSuggestion(email) {
                            const parts = forwardToInput.text.split(",");
                            parts[parts.length - 1] = email.trim();
                            forwardToInput.text = parts.map(s => s.trim()).filter(s => s !== "").join(", ") + ", ";
                            suggestions = [];
                            forwardToInput.forceActiveFocus();
                        }

                        function send() {
                            const t = DankMailService.currentThread;
                            const to = forwardToInput.text.split(",").map(s => s.trim()).filter(s => s !== "");
                            if (!t || to.length === 0 || sending)
                                return;
                            sending = true;
                            error = "";
                            DankMailService.forward(t.id, to, forwardNote.text, resp => {
                                forwardArea.sending = false;
                                if (resp.error) {
                                    forwardArea.error = resp.error;
                                    return;
                                }
                                forwardArea.reset();
                            });
                        }

                        ColumnLayout {
                            id: forwardColumn
                            anchors.left: parent.left
                            anchors.right: parent.right
                            anchors.top: parent.top
                            anchors.margins: Theme.spacingM
                            spacing: Theme.spacingS

                            DankTextField {
                                id: forwardToInput
                                Layout.fillWidth: true
                                Layout.preferredHeight: 40
                                iconName: "forward_to_inbox"
                                placeholderText: I18n.tr("Forward to… (comma-separated)", "quick forward")
                                onTextChanged: forwardSuggestDebounce.restart()
                                onAccepted: {
                                    if (forwardArea.suggestions.length > 0)
                                        forwardArea.addSuggestion(forwardArea.suggestions[0].email);
                                }

                                Timer {
                                    id: forwardSuggestDebounce
                                    interval: 200
                                    repeat: false
                                    onTriggered: {
                                        const parts = forwardToInput.text.split(",");
                                        const q = parts[parts.length - 1].trim();
                                        if (q === "") {
                                            forwardArea.suggestions = [];
                                            return;
                                        }
                                        const thread = DankMailService.currentThread;
                                        DankMailService.searchContacts(q, thread ? thread.accountId : "", list => forwardArea.suggestions = list);
                                    }
                                }
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: 2
                                visible: forwardArea.suggestions.length > 0

                                Repeater {
                                    model: forwardArea.suggestions

                                    delegate: StyledRect {
                                        id: forwardSuggestion
                                        required property var modelData
                                        Layout.fillWidth: true
                                        implicitHeight: 36
                                        radius: Theme.cornerRadiusSmall
                                        color: Theme.surfaceContainer

                                        RowLayout {
                                            anchors.fill: parent
                                            anchors.leftMargin: Theme.spacingM
                                            anchors.rightMargin: Theme.spacingM
                                            spacing: Theme.spacingS

                                            DankIcon {
                                                name: forwardSuggestion.modelData.source === "google" ? "person" : "history"
                                                size: Theme.iconSizeSmall
                                                color: Theme.surfaceTextMedium
                                            }

                                            StyledText {
                                                text: forwardSuggestion.modelData.name !== "" ? forwardSuggestion.modelData.name : forwardSuggestion.modelData.email
                                                font.pixelSize: Theme.fontSizeSmall
                                                font.weight: Font.Medium
                                            }

                                            StyledText {
                                                Layout.fillWidth: true
                                                text: forwardSuggestion.modelData.name !== "" ? forwardSuggestion.modelData.email : ""
                                                font.pixelSize: Theme.fontSizeSmall
                                                color: Theme.surfaceTextAlpha
                                                maximumLineCount: 1
                                            }
                                        }

                                        StateLayer {
                                            stateColor: Theme.primary
                                            onClicked: forwardArea.addSuggestion(forwardSuggestion.modelData.email)
                                        }
                                    }
                                }
                            }

                            StyledRect {
                                Layout.fillWidth: true
                                implicitHeight: 96
                                radius: Theme.cornerRadiusSmall
                                color: Theme.surfaceContainer
                                border.width: 1
                                border.color: forwardNote.activeFocus ? Theme.primary : Theme.outlineLight

                                DankFlickable {
                                    anchors.fill: parent
                                    anchors.margins: Theme.spacingS
                                    contentHeight: forwardNote.implicitHeight
                                    clip: true

                                    TextEdit {
                                        id: forwardNote
                                        width: parent.width
                                        wrapMode: TextEdit.Wrap
                                        textFormat: TextEdit.PlainText
                                        color: Theme.surfaceText
                                        selectionColor: Theme.primarySelected
                                        font.family: Theme.fontFamily
                                        font.pixelSize: Theme.fontSizeMedium

                                        Keys.onPressed: event => {
                                            if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) {
                                                event.accepted = true;
                                                forwardArea.send();
                                            }
                                        }

                                        StyledText {
                                            visible: forwardNote.text === "" && !forwardNote.activeFocus
                                            width: parent.width
                                            text: I18n.tr("Add a note (optional) — the message is quoted below. Ctrl+Enter sends", "quick forward")
                                            color: Theme.surfaceTextAlpha
                                            wrapMode: Text.WordWrap
                                        }
                                    }
                                }
                            }

                            RowLayout {
                                Layout.fillWidth: true
                                spacing: Theme.spacingS

                                StyledText {
                                    Layout.fillWidth: true
                                    text: forwardArea.error
                                    visible: forwardArea.error !== ""
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: Theme.error
                                    wrapMode: Text.WordWrap
                                }

                                Item {
                                    Layout.fillWidth: forwardArea.error === ""
                                }

                                StyledRect {
                                    readonly property bool ready: forwardToInput.text.trim() !== "" && !forwardArea.sending
                                    width: forwardSendLabel.implicitWidth + Theme.spacingXL
                                    height: 32
                                    radius: 16
                                    color: ready ? Theme.primaryContainer : Theme.surfaceContainerHighest
                                    opacity: ready ? 1 : 0.6

                                    StyledText {
                                        id: forwardSendLabel
                                        anchors.centerIn: parent
                                        text: forwardArea.sending ? I18n.tr("Sending…", "quick forward") : I18n.tr("Forward", "quick forward")
                                        font.pixelSize: Theme.fontSizeSmall
                                        color: parent.ready ? Theme.primary : Theme.surfaceTextMedium
                                    }

                                    StateLayer {
                                        disabled: !parent.ready
                                        stateColor: Theme.primary
                                        onClicked: forwardArea.send()
                                    }
                                }
                            }
                        }
                    }

                    StyledText {
                        Layout.fillWidth: true
                        text: bodyText.hoveredLink !== "" ? bodyText.hoveredLink : I18n.tr("Plain-text preview — open in web for the full message.", "preview footer")
                        font.pixelSize: Theme.fontSizeSmall
                        color: bodyText.hoveredLink !== "" ? Theme.primary : Theme.surfaceTextAlpha
                        elide: Text.ElideMiddle
                        maximumLineCount: 1
                    }
                }

                // Draft preview pane.
                ColumnLayout {
                    anchors.fill: parent
                    anchors.margins: Theme.spacingL
                    spacing: Theme.spacingM
                    visible: DankMailService.filterLabel === "DRAFTS" && DankMailService.currentDraft !== null

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.spacingS

                        DankIcon {
                            name: "edit_note"
                            size: Theme.iconSize
                            color: Theme.primary
                        }

                        StyledText {
                            Layout.fillWidth: true
                            text: DankMailService.currentDraft ? (DankMailService.currentDraft.subject || I18n.tr("(no subject)", "thread list")) : ""
                            font.pixelSize: Theme.fontSizeXLarge
                            font.weight: Font.DemiBold
                            maximumLineCount: 2
                        }

                        DankButton {
                            buttonHeight: 34
                            horizontalPadding: Theme.spacingM
                            text: I18n.tr("Edit draft", "compose")
                            iconName: "edit"
                            onClicked: {
                                if (DankMailService.currentDraft)
                                    composeModal.editDraft(DankMailService.currentDraft);
                            }
                        }

                        DankActionButton {
                            iconName: "delete"
                            iconColor: Theme.error
                            onClicked: {
                                if (DankMailService.currentDraft)
                                    DankMailService.deleteDraft(DankMailService.currentDraft.id, null);
                            }
                        }
                    }

                    Flow {
                        Layout.fillWidth: true
                        spacing: 4
                        visible: DankMailService.currentDraft !== null && (DankMailService.currentDraft.to || []).length > 0

                        StyledText {
                            text: I18n.tr("To", "preview header") + ":"
                            font.pixelSize: Theme.fontSizeSmall
                            font.weight: Font.Medium
                            color: Theme.surfaceTextMedium
                        }

                        Repeater {
                            model: DankMailService.currentDraft ? (DankMailService.currentDraft.to || []) : []

                            delegate: StyledRect {
                                id: draftToChip
                                required property string modelData
                                width: draftToRow.implicitWidth + Theme.spacingM
                                height: 22
                                radius: 11
                                color: Theme.surfaceContainerHigh

                                RowLayout {
                                    id: draftToRow
                                    anchors.centerIn: parent
                                    spacing: 4

                                    StyledText {
                                        text: DankMailService.formatAddressFull(draftToChip.modelData)
                                        font.pixelSize: Theme.fontSizeSmall
                                    }

                                    DankIcon {
                                        name: "content_copy"
                                        size: 12
                                        color: Theme.surfaceTextAlpha
                                    }
                                }

                                StateLayer {
                                    stateColor: Theme.primary
                                    onClicked: window.copyWithFeedback(DankMailService.parseAddress(draftToChip.modelData).email || draftToChip.modelData)
                                }
                            }
                        }
                    }

                    Flow {
                        Layout.fillWidth: true
                        spacing: Theme.spacingXS
                        visible: DankMailService.currentDraft !== null && (DankMailService.currentDraft.attachments || []).length > 0

                        Repeater {
                            model: DankMailService.currentDraft ? (DankMailService.currentDraft.attachments || []) : []

                            delegate: StyledRect {
                                id: draftAttPreviewChip
                                required property var modelData
                                width: draftAttPreviewRow.implicitWidth + Theme.spacingL
                                height: 28
                                radius: 14
                                color: Theme.surfaceContainerHigh
                                border.width: 1
                                border.color: Theme.outlineMedium

                                RowLayout {
                                    id: draftAttPreviewRow
                                    anchors.centerIn: parent
                                    spacing: Theme.spacingXS

                                    DankIcon {
                                        name: "attach_file"
                                        size: Theme.iconSizeSmall
                                        color: Theme.primary
                                    }

                                    StyledText {
                                        text: draftAttPreviewChip.modelData.filename + (window.formatSize(draftAttPreviewChip.modelData.size) !== "" ? "  ·  " + window.formatSize(draftAttPreviewChip.modelData.size) : "")
                                        font.pixelSize: Theme.fontSizeSmall
                                        maximumLineCount: 1
                                    }
                                }
                            }
                        }
                    }

                    Rectangle {
                        Layout.fillWidth: true
                        Layout.preferredHeight: 1
                        color: Theme.outlineMedium
                    }

                    DankFlickable {
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        contentHeight: draftBodyPreview.implicitHeight + Theme.spacingL * 2
                        clip: true

                        TextEdit {
                            id: draftBodyPreview
                            width: Math.min(parent.width - Theme.spacingL * 2, 760)
                            x: (parent.width - width) / 2
                            y: Theme.spacingL
                            readOnly: true
                            selectByMouse: true
                            wrapMode: TextEdit.Wrap
                            textFormat: TextEdit.RichText
                            color: Theme.surfaceText
                            selectionColor: Theme.primarySelected
                            font.family: Theme.fontFamily
                            font.pixelSize: Theme.fontSizeMedium
                            text: DankMailService.currentDraft ? window.formatBody(DankMailService.currentDraft.body || "") : ""
                            onLinkActivated: link => window.openBodyLink(link)

                            HoverHandler {
                                cursorShape: draftBodyPreview.hoveredLink !== "" ? Qt.PointingHandCursor : Qt.IBeamCursor
                            }
                        }
                    }
                }

                // Preview empty state.
                ColumnLayout {
                    anchors.centerIn: parent
                    visible: DankMailService.filterLabel === "DRAFTS" ? (DankMailService.currentDraft === null) : (DankMailService.currentThread === null)
                    spacing: Theme.spacingM

                    DankIcon {
                        Layout.alignment: Qt.AlignHCenter
                        name: "drafts"
                        size: 48
                        color: Theme.surfaceTextAlpha
                    }

                    StyledText {
                        Layout.alignment: Qt.AlignHCenter
                        text: I18n.tr("Select a thread", "empty preview")
                        color: Theme.surfaceTextMedium
                    }
                }
            }
        }
    }

    // Floating Undo Toast/Banner (Material 3 style)
    StyledRect {
        id: undoBanner
        visible: window.undoAction !== null
        
        anchors.bottom: parent.bottom
        anchors.bottomMargin: Theme.spacingL
        anchors.horizontalCenter: parent.horizontalCenter
        z: 999
        
        width: undoRow.implicitWidth + Theme.spacingXL
        height: 40
        radius: 8
        color: "#2d3139" // Premium dark slate background
        border.width: 1
        border.color: Theme.outline

        RowLayout {
            id: undoRow
            anchors.centerIn: parent
            spacing: Theme.spacingM

            StyledText {
                text: {
                    if (!window.undoAction) return "";
                    const count = window.undoAction.ids.length;
                    const type = window.undoAction.type;
                    if (type === "archive") {
                        return count === 1 ? I18n.tr("1 thread archived.", "undo banner") : I18n.tr("%1 threads archived.", "undo banner").arg(count);
                    } else if (type === "trash") {
                        return count === 1 ? I18n.tr("1 thread deleted.", "undo banner") : I18n.tr("%1 threads deleted.", "undo banner").arg(count);
                    } else if (type === "unspam") {
                        return count === 1 ? I18n.tr("1 thread moved to Inbox.", "undo banner") : I18n.tr("%1 threads moved to Inbox.", "undo banner").arg(count);
                    } else if (type === "spam") {
                        return count === 1 ? I18n.tr("1 thread marked as Spam.", "undo banner") : I18n.tr("%1 threads marked as Spam.", "undo banner").arg(count);
                    } else if (type === "snooze") {
                        return count === 1 ? I18n.tr("1 thread snoozed.", "undo banner") : I18n.tr("%1 threads snoozed.", "undo banner").arg(count);
                    }
                    return "";
                }
                color: "#f5f6f9" // High contrast light text
                font.pixelSize: Theme.fontSizeSmall
            }

            // Undo action button
            StyledRect {
                width: undoText.implicitWidth + Theme.spacingM
                height: 24
                radius: 4
                color: "transparent"

                StyledText {
                    id: undoText
                    anchors.centerIn: parent
                    text: I18n.tr("Undo", "undo banner")
                    color: Theme.primary
                    font.pixelSize: Theme.fontSizeSmall
                    font.weight: Font.Bold
                }

                StateLayer {
                    stateColor: Theme.primary
                    onClicked: window.triggerUndo()
                }
            }
        }
    }
}
