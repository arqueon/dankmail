import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Common
import qs.Modals
import qs.Services
import qs.Widgets

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

    function selectThread(t) {
        selectedThreadId = t.id;
        replyArea.reset();
        forwardArea.reset();
        DankMailService.loadThread(t.id, true);
    }

    function selectedIds() {
        return selectedThreadId >= 0 ? [selectedThreadId] : [];
    }

    // Spam-review multi-selection: ids checked for a batch rescue.
    // Reassigned whole on every toggle so bindings re-evaluate.
    property var spamChecked: []

    function toggleSpamChecked(id) {
        const i = spamChecked.indexOf(id);
        const next = spamChecked.slice();
        if (i === -1)
            next.push(id);
        else
            next.splice(i, 1);
        spamChecked = next;
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

    function escapeHtml(s) {
        return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    }

    function makeAnchor(url, label) {
        return `<a href="${url.replace(/&amp;/g, "&")}"><font color="${Theme.primary}">${label}</font></a>`;
    }

    // linkify wraps bare http(s)/mailto URLs in already-escaped text,
    // shortening very long ones for display (href keeps the full URL).
    function linkify(escaped) {
        return escaped.replace(/(https?:\/\/[^\s&]*(?:&amp;[^\s&]*)*|mailto:[^\s<]+)/g, url => {
            const label = url.length > 64 ? url.substring(0, 60) + "…" : url;
            return makeAnchor(url, label);
        });
    }

    // renderInline turns the light markdown that the HTML→text distiller
    // emits into presentation markup: [text](url) becomes a clickable
    // label (URL hidden), image-buttons collapse to their alt text,
    // **bold** becomes bold, and leftover bare URLs get linkified.
    // Anchors are stashed behind \x01N\x01 placeholders so later passes
    // never touch URLs already inside an href.
    function renderInline(escaped) {
        let s = escaped.replace(/\\([\\`*_{}\[\]()#+\-.!~])/g, "$1"); // markdown escapes
        const stash = [];
        const put = html => {
            stash.push(html);
            return "\x01" + (stash.length - 1) + "\x01";
        };
        const boldify = t => t.replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>");

        // [![alt](img)](url) — image buttons (Guardar/Twitter/…): keep a
        // small labeled link, drop the image.
        s = s.replace(/\[!\[([^\]]*)\]\(([^()\s]+)\)\]\(([^()\s]+)\)/g, (m, alt, img, url) => put(makeAnchor(url, (alt !== "" ? alt : "enlace") + " ↗")));
        // ![alt](img) — plain images: alt text only.
        s = s.replace(/!\[([^\]]*)\]\(([^()\s]+)\)/g, (m, alt) => alt);
        // [text](url) — regular links: clickable label, URL hidden.
        s = s.replace(/\[([^\]]+)\]\(([^()\s]+)\)/g, (m, txt, url) => put(makeAnchor(url, boldify(txt))));

        s = boldify(s);
        s = linkify(s);
        return s.replace(/\x01(\d+)\x01/g, (m, i) => stash[i]);
    }

    // formatBody turns the plain-text body into our own presentation
    // markup (this is NOT rendering the email's HTML — the source stays
    // plain text): quote levels ('>', '>>', …) become indented, colored
    // blocks with the markers stripped, and URLs become clickable.
    function formatBody(text) {
        const quoteColors = [String(Theme.surfaceText), String(Theme.primary), String(Theme.success), String(Theme.warning), String(Theme.secondary)];
        const lines = text.split("\n");
        let html = "";
        let depth = 0;
        for (let i = 0; i < lines.length; i++) {
            const m = lines[i].match(/^\s*((?:>\s?)+)/);
            const d = m ? (m[1].match(/>/g) || []).length : 0;
            const content = m ? lines[i].substring(m[0].length) : lines[i];
            while (depth < d) {
                html += "<blockquote>";
                depth++;
            }
            while (depth > d) {
                html += "</blockquote>";
                depth--;
            }
            const color = quoteColors[Math.min(d, quoteColors.length - 1)];
            let body = renderInline(escapeHtml(content));
            // Markdown headings from the distiller → bold lines.
            const h = body.match(/^\s*#{1,6}\s+(.*)$/);
            if (h)
                body = "<b>" + h[1] + "</b>";
            // One paragraph per source line (not <br>) so each line is its
            // own text block: triple-click then selects the line instead of
            // the whole body.
            html += `<p style="margin:0"><font color="${color}">${body === "" ? "&nbsp;" : body}</font></p>`;
        }
        while (depth > 0) {
            html += "</blockquote>";
            depth--;
        }
        return html;
    }

    // Entry points for the shell's pending-intent dispatch (ui.showThread
    // / ui.compose events may arrive before this window exists).
    function openThread(threadId) {
        selectedThreadId = threadId;
        replyArea.reset();
        forwardArea.reset();
        DankMailService.loadThread(threadId, true);
    }

    function openCompose() {
        composeModal.show();
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

                // Quick filters: all / unread / starred.
                Row {
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
                                "key": "spam",
                                "label": I18n.tr("Spam", "filter")
                            }
                        ]

                        delegate: StyledRect {
                            required property var modelData
                            readonly property bool active: (modelData.key === "unread" && DankMailService.filterUnread) || (modelData.key === "starred" && DankMailService.filterStarred) || (modelData.key === "spam" && DankMailService.filterLabel === "SPAM") || (modelData.key === "all" && !DankMailService.filterUnread && !DankMailService.filterStarred && DankMailService.filterLabel === "")

                            width: filterLabel.implicitWidth + Theme.spacingL
                            height: 30
                            radius: 15
                            color: active ? Theme.primaryContainer : "transparent"

                            StyledText {
                                id: filterLabel
                                anchors.centerIn: parent
                                text: parent.modelData.label
                                font.pixelSize: Theme.fontSizeSmall
                                color: parent.active ? Theme.primary : Theme.surfaceTextMedium
                            }

                            StateLayer {
                                stateColor: Theme.primary
                                onClicked: {
                                    DankMailService.filterUnread = parent.modelData.key === "unread";
                                    DankMailService.filterStarred = parent.modelData.key === "starred";
                                    DankMailService.filterLabel = parent.modelData.key === "spam" ? "SPAM" : "";
                                    window.spamChecked = [];
                                    DankMailService.refreshThreads();
                                }
                            }
                        }
                    }

                    // Spam review: one click marks everything listed as
                    // read, so the folder can be left "reviewed".
                    DankActionButton {
                        visible: DankMailService.filterLabel === "SPAM" && DankMailService.threads.some(t => t.unread)
                        iconName: "done_all"
                        iconColor: Theme.primary
                        onClicked: {
                            const ids = DankMailService.threads.filter(t => t.unread).map(t => t.id);
                            if (ids.length)
                                DankMailService.markRead(ids);
                        }
                    }

                    // "Not spam": rescue the checked threads back to the
                    // inbox. Count chip + button appear with a selection.
                    StyledRect {
                        visible: DankMailService.filterLabel === "SPAM" && window.spamChecked.length > 0
                        width: rescueRow.implicitWidth + Theme.spacingL
                        height: 30
                        radius: 15
                        color: Theme.primaryContainer

                        RowLayout {
                            id: rescueRow
                            anchors.centerIn: parent
                            spacing: Theme.spacingXS

                            DankIcon {
                                name: "move_to_inbox"
                                size: Theme.iconSizeSmall
                                color: Theme.primary
                            }

                            StyledText {
                                text: I18n.tr("Not spam", "spam review") + " (" + window.spamChecked.length + ")"
                                font.pixelSize: Theme.fontSizeSmall
                                color: Theme.primary
                            }
                        }

                        StateLayer {
                            stateColor: Theme.primary
                            onClicked: {
                                DankMailService.unspam(window.spamChecked);
                                window.spamChecked = [];
                            }
                        }
                    }
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
                    Layout.preferredWidth: 220
                    Layout.preferredHeight: 36
                    iconName: "search"
                    placeholderText: I18n.tr("Search mail…", "search")
                    onTextChanged: searchDebounce.restart()

                    Timer {
                        id: searchDebounce
                        interval: 250
                        repeat: false
                        onTriggered: {
                            DankMailService.searchQuery = searchField.text.trim();
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

        // ---- content: list + preview ------------------------------------
        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            // Thread list.
            Item {
                Layout.preferredWidth: 400
                Layout.fillHeight: true

                ListView {
                    id: threadList
                    anchors.fill: parent
                    clip: true
                    model: DankMailService.threads
                    boundsBehavior: Flickable.StopAtBounds
                    spacing: 0

                    delegate: Rectangle {
                        id: row
                        required property var modelData

                        readonly property bool selected: modelData.id === window.selectedThreadId

                        width: threadList.width
                        height: 76
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

                            // Spam review: reserve room for the checkbox
                            // overlay (declared after the row's StateLayer
                            // so it actually receives clicks).
                            Item {
                                visible: DankMailService.filterLabel === "SPAM"
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

                        // Spam review checkbox: square check for the batch
                        // "not spam" rescue. Declared after the row's
                        // StateLayer so its clicks aren't swallowed by the
                        // row selection.
                        Rectangle {
                            visible: DankMailService.filterLabel === "SPAM"
                            readonly property bool checked: window.spamChecked.indexOf(row.modelData.id) !== -1
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
                                DankMailService.snooze([row.modelData.id], window.snoozeUntil(key));
                                clearIfOpen();
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
                                        DankMailService.archive([row.modelData.id]);
                                        rowActions.clearIfOpen();
                                    }
                                }

                                DankActionButton {
                                    buttonSize: 30
                                    iconName: "delete"
                                    iconColor: Theme.error
                                    onClicked: {
                                        DankMailService.trash([row.modelData.id]);
                                        rowActions.clearIfOpen();
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

                // Empty state.
                ColumnLayout {
                    anchors.centerIn: parent
                    visible: DankMailService.threads.length === 0
                    spacing: Theme.spacingM

                    DankIcon {
                        Layout.alignment: Qt.AlignHCenter
                        name: DankMailService.connected ? "inbox" : "cloud_off"
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
                            if (DankMailService.searchQuery !== "")
                                return I18n.tr("No local results — search the full history or the web", "empty state");
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
                    visible: DankMailService.currentThread !== null

                    StyledText {
                        Layout.fillWidth: true
                        text: DankMailService.currentThread ? (DankMailService.currentThread.subject || I18n.tr("(no subject)", "thread list")) : ""
                        font.pixelSize: Theme.fontSizeXLarge
                        font.weight: Font.DemiBold
                        maximumLineCount: 2
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.spacingS

                        Rectangle {
                            width: 10
                            height: 10
                            radius: 5
                            color: DankMailService.currentThread ? DankMailService.accountColor(DankMailService.currentThread.accountId) : "transparent"
                        }

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: Theme.spacingS

                            readonly property var lastMsg: {
                                const t = DankMailService.currentThread;
                                if (!t || !t.messages || t.messages.length === 0)
                                    return null;
                                return t.messages[t.messages.length - 1];
                            }
                            readonly property var fromAddr: lastMsg ? DankMailService.parseAddress(lastMsg.from) : null
                            id: fromLine

                            StyledText {
                                text: fromLine.fromAddr ? (fromLine.fromAddr.name !== "" ? fromLine.fromAddr.name : fromLine.fromAddr.email) : ""
                                font.weight: Font.DemiBold
                                font.pixelSize: Theme.fontSizeMedium
                            }

                            StyledText {
                                Layout.fillWidth: true
                                text: {
                                    if (!fromLine.fromAddr)
                                        return "";
                                    const addr = fromLine.fromAddr.name !== "" ? fromLine.fromAddr.email : "";
                                    const when = Qt.formatDateTime(new Date(fromLine.lastMsg.date), "ddd d MMM, HH:mm");
                                    return (addr !== "" ? addr + "  ·  " : "") + when;
                                }
                                font.pixelSize: Theme.fontSizeSmall
                                color: Theme.surfaceTextAlpha
                                maximumLineCount: 1
                            }
                        }
                    }

                    // Recipients of the shown (newest) message. BCC of
                    // received mail never travels in the headers, so when
                    // the account's own address is in neither To nor CC the
                    // message very likely arrived via BCC — say so.
                    ColumnLayout {
                        id: rcptLines
                        Layout.fillWidth: true
                        spacing: 0
                        visible: fromLine.lastMsg !== null && (toList.length > 0 || ccList.length > 0 || bccLikely)

                        readonly property var toList: fromLine.lastMsg ? (fromLine.lastMsg.to || []) : []
                        readonly property var ccList: fromLine.lastMsg ? (fromLine.lastMsg.cc || []) : []
                        readonly property string ownEmail: {
                            const t = DankMailService.currentThread;
                            if (!t)
                                return "";
                            const accs = DankMailService.accounts || [];
                            for (let i = 0; i < accs.length; i++) {
                                if (accs[i].id === t.accountId)
                                    return String(accs[i].email || "").toLowerCase();
                            }
                            return "";
                        }
                        readonly property bool bccLikely: {
                            if (ownEmail === "" || !fromLine.lastMsg)
                                return false;
                            const all = toList.concat(ccList);
                            for (let i = 0; i < all.length; i++) {
                                if (String(all[i]).toLowerCase().indexOf(ownEmail) !== -1)
                                    return false;
                            }
                            return true;
                        }

                        StyledText {
                            Layout.fillWidth: true
                            visible: rcptLines.toList.length > 0
                            text: I18n.tr("To", "preview header") + ": " + rcptLines.toList.join(", ")
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.surfaceTextAlpha
                            elide: Text.ElideRight
                            maximumLineCount: 1
                        }

                        StyledText {
                            Layout.fillWidth: true
                            visible: rcptLines.ccList.length > 0
                            text: I18n.tr("Cc", "preview header") + ": " + rcptLines.ccList.join(", ")
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.surfaceTextAlpha
                            elide: Text.ElideRight
                            maximumLineCount: 1
                        }

                        StyledText {
                            Layout.fillWidth: true
                            visible: rcptLines.bccLikely
                            text: I18n.tr("Received via BCC — your address is not in To or Cc", "preview header")
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.warning
                            elide: Text.ElideRight
                            maximumLineCount: 1
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

                    // Action bar — archive leads, Gmail-style.
                    RowLayout {
                        spacing: Theme.spacingXS

                        DankActionButton {
                            iconName: "archive"
                            onClicked: {
                                const ids = window.selectedIds();
                                if (ids.length) {
                                    DankMailService.archive(ids);
                                    DankMailService.currentThread = null;
                                }
                            }
                        }

                        DankActionButton {
                            iconName: "delete"
                            iconColor: Theme.error
                            onClicked: {
                                const ids = window.selectedIds();
                                if (ids.length) {
                                    DankMailService.trash(ids);
                                    DankMailService.currentThread = null;
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

                        Item {
                            Layout.fillWidth: true
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
                                DankMailService.snooze(ids, until);
                                DankMailService.currentThread = null;
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

                    // Plain-text body of the newest message.
                    DankFlickable {
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        contentHeight: bodyText.implicitHeight
                        clip: true

                        TextEdit {
                            id: bodyText
                            width: parent.width
                            readOnly: true
                            selectByMouse: true
                            wrapMode: TextEdit.Wrap
                            textFormat: TextEdit.RichText
                            color: Theme.surfaceText
                            selectionColor: Theme.primarySelected
                            font.family: Theme.fontFamily
                            font.pixelSize: Theme.fontSizeMedium
                            text: {
                                const t = DankMailService.currentThread;
                                if (!t || !t.messages || t.messages.length === 0)
                                    return "";
                                return window.formatBody(t.messages[t.messages.length - 1].bodyText || "");
                            }
                            onLinkActivated: link => {
                                if (link.startsWith("http://") || link.startsWith("https://") || link.startsWith("mailto:"))
                                    Qt.openUrlExternally(link);
                            }

                            HoverHandler {
                                enabled: bodyText.hoveredLink !== ""
                                cursorShape: Qt.PointingHandCursor
                            }
                        }
                    }

                    // Quick reply (spec §7): plain textarea, Ctrl+Enter
                    // sends, reply-all as a toggle. No CC/BCC, no
                    // attachments, no rich signature — by design.
                    StyledRect {
                        id: replyArea
                        visible: false
                        Layout.fillWidth: true
                        implicitHeight: replyColumn.implicitHeight + Theme.spacingL
                        color: Theme.surfaceContainerHigh

                        property bool sending: false
                        property bool replyAll: false
                        property string error: ""

                        function reset() {
                            visible = false;
                            sending = false;
                            replyAll = false;
                            error = "";
                            replyInput.text = "";
                        }

                        function send() {
                            const t = DankMailService.currentThread;
                            if (!t || replyInput.text.trim() === "" || sending)
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
                            });
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
                                    readonly property bool ready: replyInput.text.trim() !== "" && !replyArea.sending
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

                        function reset() {
                            visible = false;
                            sending = false;
                            error = "";
                            forwardToInput.text = "";
                            forwardNote.text = "";
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
                        text: I18n.tr("Plain-text preview — open in web for the full message.", "preview footer")
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.surfaceTextAlpha
                    }
                }

                // Preview empty state.
                ColumnLayout {
                    anchors.centerIn: parent
                    visible: DankMailService.currentThread === null
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
}
