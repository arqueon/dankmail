import QtQuick
import Quickshell
import Quickshell.Io
import qs.Common
import qs.Modules.Plugins
import qs.Widgets

// Unread badge for dankmail (https://github.com/arqueon/dankmail).
// Live updates: subscribes to the dmail daemon's IPC socket (line-JSON
// protocol) instead of polling. Left click toggles the triage window
// (the daemon relaunches the UI if it was closed); right click syncs.
// If the daemon is down, the icon dims and a click starts the service.
PluginComponent {
    id: root

    property bool hideWhenZero: pluginData.hideWhenZero ?? false
    property bool showDndDot: pluginData.showDndDot ?? true

    property bool daemonConnected: false
    property int unread: 0
    property bool dnd: false
    property var accounts: []
    property var threads: []
    property bool syncing: false
    property int _reqId: 0
    property int _statusReqId: -1
    property int _threadsReqId: -1
    property int _syncReqId: -1

    readonly property string socketPath: {
        const rt = Quickshell.env("XDG_RUNTIME_DIR");
        return (rt && rt !== "" ? rt : "/tmp") + "/dankmail.sock";
    }

    readonly property bool pillHidden: hideWhenZero && daemonConnected && unread === 0

    // toggleApp is PATH-proof: the DMS process may not have ~/.local/bin
    // in PATH, so try the user install location first. A failed exec
    // kills the shell (|| never runs after it), so test before exec.
    // toggle (not show) so a second middle click hides the window again,
    // matching dms-dankcalendar's click model.
    function toggleApp() {
        if (root.daemonConnected)
            Quickshell.execDetached(["sh", "-c", "[ -x \"$HOME/.local/bin/dmail\" ] && exec \"$HOME/.local/bin/dmail\" toggle; exec dmail toggle"]);
        else
            Quickshell.execDetached(["systemctl", "--user", "start", "dmail"]);
    }

    function send(sock, obj) {
        sock.write(JSON.stringify(obj) + "\n");
        sock.flush();
    }

    function refreshStatus() {
        if (!cmdSocket.connected)
            return;
        root._reqId++;
        root._statusReqId = root._reqId;
        send(cmdSocket, {
            "id": root._statusReqId,
            "method": "system.status",
            "params": {}
        });
        root._reqId++;
        root._threadsReqId = root._reqId;
        send(cmdSocket, {
            "id": root._threadsReqId,
            "method": "threads.list",
            "params": {
                "inbox": true,
                "limit": 20
            }
        });
    }

    // op fires a thread action from the popout; the daemon's optimistic
    // apply + our event subscription refresh the list right after.
    function op(method, threadId) {
        if (!cmdSocket.connected)
            return;
        root._reqId++;
        send(cmdSocket, {
            "id": root._reqId,
            "method": method,
            "params": {
                "ids": [threadId]
            }
        });
    }

    function uiCall(method, params) {
        if (!cmdSocket.connected)
            return;
        root._reqId++;
        send(cmdSocket, {
            "id": root._reqId,
            "method": method,
            "params": params || {}
        });
    }

    function senderOf(t) {
        const raw = (t.participants && t.participants.length > 0) ? t.participants[0] : "";
        const m = raw.match(/^\s*"?([^"<]*?)"?\s*<[^>]+>\s*$/);
        return m && m[1].trim() !== "" ? m[1].trim() : raw;
    }

    function timeOf(iso) {
        const d = new Date(iso);
        const now = new Date();
        if (d.toDateString() === now.toDateString())
            return Qt.formatTime(d, "HH:mm");
        return Qt.formatDate(d, "d MMM");
    }

    // Left click opens the popout (automatic when popoutContent is set);
    // right click syncs, with visible feedback while it runs.
    pillRightClickAction: () => root.syncNow()

    function syncNow() {
        if (!cmdSocket.connected || root.syncing)
            return;
        root._reqId++;
        root._syncReqId = root._reqId;
        root.syncing = true;
        syncGuard.restart();
        send(cmdSocket, {
            "id": root._syncReqId,
            "method": "system.sync",
            "params": {}
        });
    }

    Component.onCompleted: cmdSocket.connected = true

    // Command connection: handshake line first, then request/response.
    Socket {
        id: cmdSocket

        path: root.socketPath
        connected: false

        onConnectionStateChanged: {
            root.daemonConnected = connected;
            if (connected) {
                subSocket.connected = true;
                root.refreshStatus();
            } else {
                subSocket.connected = false;
                root.unread = 0;
                root.accounts = [];
                retryTimer.restart();
            }
        }

        parser: SplitParser {
            onRead: line => {
                if (!line || line.length === 0)
                    return;
                root._lastSeen = Date.now();
                let msg;
                try {
                    msg = JSON.parse(line);
                } catch (e) {
                    return;
                }
                if (msg.id === undefined)
                    return;
                if (msg.id === root._syncReqId) {
                    root.syncing = false;
                    syncGuard.stop();
                    return;
                }
                if (msg.id === root._statusReqId && msg.result) {
                    root.unread = msg.result.unread || 0;
                    root.dnd = !!msg.result.dnd;
                    root.accounts = msg.result.accounts || [];
                } else if (msg.id === root._threadsReqId) {
                    root.threads = msg.result || [];
                }
            }
        }
    }

    // Subscription connection: daemon events push refreshes.
    Socket {
        id: subSocket

        path: root.socketPath
        connected: false

        onConnectionStateChanged: {
            if (connected)
                root.send(subSocket, {
                    "id": 1,
                    "method": "subscribe"
                });
        }

        parser: SplitParser {
            onRead: line => {
                if (!line || line.length === 0)
                    return;
                let ev;
                try {
                    ev = JSON.parse(line);
                } catch (e) {
                    return;
                }
                switch (ev.topic) {
                case "threads.changed":
                case "unread.changed":
                case "ops.applied":
                case "accounts.changed":
                case "snooze.woke":
                    refreshDebounce.restart();
                    break;
                case "dnd.changed":
                    root.dnd = !!(ev.payload && ev.payload.enabled);
                    break;
                }
            }
        }
    }

    // Clears the syncing indicator if the daemon never answers.
    Timer {
        id: syncGuard
        interval: 30000
        repeat: false
        onTriggered: root.syncing = false
    }

    property double _lastSeen: 0

    // Reconnect with the explicit false→true toggle (the raw Socket can
    // keep a stale 'connected' after the peer closes — dcal's DankSocket
    // exists for the same reason).
    function reconnect() {
        cmdSocket.connected = false;
        subSocket.connected = false;
        Qt.callLater(() => {
            cmdSocket.connected = true;
        });
    }

    Timer {
        id: retryTimer
        interval: 4000
        repeat: false
        onTriggered: root.reconnect()
    }

    Timer {
        id: refreshDebounce
        interval: 300
        repeat: false
        onTriggered: root.refreshStatus()
    }

    // Safety poll — also detects zombie sockets: if no line arrived in
    // 90s despite polling every 60s, the connection is dead; recycle it.
    Timer {
        interval: 60000
        running: root.daemonConnected
        repeat: true
        onTriggered: {
            if (root._lastSeen > 0 && Date.now() - root._lastSeen > 90000) {
                root.reconnect();
                return;
            }
            root.refreshStatus();
        }
    }

    readonly property string unreadLabel: unread > 99 ? "99+" : String(unread)

    horizontalBarPill: Component {
        Item {
            implicitWidth: root.pillHidden ? 0 : hRow.implicitWidth
            implicitHeight: hRow.implicitHeight
            visible: !root.pillHidden

            // Middle click on the pill: toggle the app window (left
            // opens the popout, right syncs). Only MiddleButton is
            // accepted, so left/right fall through to BasePill.
            MouseArea {
                anchors.fill: parent
                // Cover BasePill's padding too — middle clicks on the
                // capsule margin were falling through to the bar canvas.
                anchors.margins: -10
                acceptedButtons: Qt.MiddleButton
                onClicked: root.toggleApp()
            }

            Row {
                id: hRow
                spacing: Theme.spacingXS
                anchors.verticalCenter: parent.verticalCenter

                Item {
                    width: root.iconSize
                    height: root.iconSize
                    anchors.verticalCenter: parent.verticalCenter

                    DankIcon {
                        anchors.fill: parent
                        name: root.syncing ? "sync" : (root.dnd ? "notifications_off" : "mail")
                        size: root.iconSize
                        color: {
                            if (!root.daemonConnected)
                                return Theme.surfaceVariantText;
                            return Theme.primary;
                        }
                    }

                    Rectangle {
                        visible: root.showDndDot && root.dnd && root.daemonConnected
                        width: 7
                        height: 7
                        radius: 3.5
                        color: Theme.warning
                        anchors.right: parent.right
                        anchors.top: parent.top
                    }
                }

                NumericText {
                    visible: root.daemonConnected && root.unread > 0
                    text: root.unreadLabel
                    reserveText: "99+"
                    width: reservedWidth
                    font.pixelSize: Theme.fontSizeSmall
                    font.weight: Font.Bold
                    color: Theme.primary
                    horizontalAlignment: Text.AlignHCenter
                    anchors.verticalCenter: parent.verticalCenter
                }
            }
        }
    }

    popoutWidth: 440
    popoutHeight: 520
    popoutContent: Component {
        PopoutComponent {
            id: popout

            // Custom header (the built-in one hides with empty headerText):
            // the title itself opens the app, wherever the focus is.
            Item {
                width: parent.width
                height: 48

                // Clickable title zone.
                Column {
                    id: titleZone
                    anchors.left: parent.left
                    anchors.leftMargin: Theme.spacingS
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 0

                    StyledText {
                        text: "Dank Mail"
                        font.pixelSize: Theme.fontSizeLarge + 2
                        font.weight: Font.Bold
                        color: titleHover.hovered ? Theme.primary : Theme.surfaceText
                    }

                    StyledText {
                        text: root.daemonConnected ? ((root.accounts.length > 1 ? root.accounts.length + " cuentas · " : "") + (root.unread > 0 ? root.unread + " sin leer" : "Bandeja en cero")) : "daemon apagado"
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.surfaceVariantText
                    }

                    HoverHandler {
                        id: titleHover
                        cursorShape: Qt.PointingHandCursor
                    }

                    TapHandler {
                        onTapped: {
                            root.toggleApp();
                            if (popout.closePopout)
                                popout.closePopout();
                        }
                    }
                }

                // Action buttons sit above the title's tap zone.
                Row {
                    spacing: Theme.spacingXS
                    anchors.right: parent.right
                    anchors.rightMargin: Theme.spacingXS
                    anchors.verticalCenter: parent.verticalCenter

                    DankActionButton {
                        iconName: "edit_square"
                        visible: root.daemonConnected
                        onClicked: {
                            root.uiCall("ui.compose", {});
                            if (popout.closePopout)
                                popout.closePopout();
                        }
                    }

                    DankActionButton {
                        iconName: "sync"
                        iconColor: root.syncing ? Theme.primary : Theme.surfaceText
                        visible: root.daemonConnected
                        onClicked: root.syncNow()
                    }

                    DankActionButton {
                        iconName: root.dnd ? "notifications_off" : "notifications"
                        iconColor: root.dnd ? Theme.warning : Theme.surfaceText
                        visible: root.daemonConnected
                        onClicked: root.uiCall(root.dnd ? "dnd.off" : "dnd.on", {})
                    }

                    DankActionButton {
                        iconName: "close"
                        onClicked: {
                            if (popout.closePopout)
                                popout.closePopout();
                        }
                    }
                }
            }

            Column {
                width: parent.width
                visible: root.daemonConnected && root.accounts.length > 1
                spacing: 2

                Repeater {
                    model: root.accounts

                    delegate: Row {
                        required property var modelData
                        width: parent.width
                        spacing: Theme.spacingS

                        DankIcon {
                            name: "account_circle"
                            size: Theme.iconSizeSmall
                            color: Theme.primary
                            anchors.verticalCenter: parent.verticalCenter
                        }

                        StyledText {
                            width: parent.width - accountUnread.implicitWidth - Theme.iconSizeSmall - Theme.spacingS * 2
                            text: parent.modelData.displayName || parent.modelData.email || "Cuenta"
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.surfaceVariantText
                            elide: Text.ElideRight
                            anchors.verticalCenter: parent.verticalCenter
                        }

                        StyledText {
                            id: accountUnread
                            text: (parent.modelData.unread || 0) + " sin leer"
                            font.pixelSize: Theme.fontSizeSmall
                            font.weight: Font.Medium
                            color: parent.modelData.unread > 0 ? Theme.primary : Theme.surfaceVariantText
                            anchors.verticalCenter: parent.verticalCenter
                        }
                    }
                }
            }

            Item {
                width: parent.width
                // The list scrolls inside a fixed viewport when it grows
                // beyond the popout.
                readonly property real maxListHeight: root.accounts.length > 1 ? 350 : 430
                implicitHeight: Math.min(threadColumn.implicitHeight + Theme.spacingM * 2, maxListHeight)

                DankFlickable {
                    anchors.fill: parent
                    anchors.margins: Theme.spacingS
                    contentHeight: threadColumn.implicitHeight
                    clip: true

                    Column {
                        id: threadColumn
                        width: parent.width
                        spacing: 2

                    StyledText {
                        visible: !root.daemonConnected
                        width: parent.width
                        text: "El daemon de dankmail no está corriendo. Usa el botón ↗ para iniciarlo."
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.surfaceVariantText
                        wrapMode: Text.WordWrap
                    }

                    StyledText {
                        visible: root.daemonConnected && root.threads.length === 0
                        width: parent.width
                        text: "Sin correos en la bandeja."
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.surfaceVariantText
                    }

                    Repeater {
                        model: root.threads

                        delegate: Rectangle {
                            id: mailRow
                            required property var modelData

                            width: threadColumn.width
                            height: 56
                            radius: Theme.cornerRadiusSmall
                            color: rowHover.hovered ? Theme.surfaceContainerHigh : "transparent"

                            HoverHandler {
                                id: rowHover
                            }

                            // Row body: unread dot, sender/subject, star, time.
                            Row {
                                anchors.left: parent.left
                                anchors.right: rowActions.visible ? rowActions.left : parent.right
                                anchors.leftMargin: Theme.spacingS
                                anchors.rightMargin: Theme.spacingS
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: Theme.spacingS

                                Rectangle {
                                    width: 8
                                    height: 8
                                    radius: 4
                                    color: mailRow.modelData.unread ? Theme.primary : "transparent"
                                    anchors.verticalCenter: parent.verticalCenter
                                }

                                Column {
                                    width: parent.width - 8 - Theme.spacingS * 2 - timeLabel.implicitWidth - (starMark.visible ? starMark.width + Theme.spacingS : 0)
                                    spacing: 1

                                    StyledText {
                                        width: parent.width
                                        text: root.senderOf(mailRow.modelData)
                                        font.pixelSize: Theme.fontSizeSmall
                                        font.weight: mailRow.modelData.unread ? Font.Bold : Font.Normal
                                        color: mailRow.modelData.unread ? Theme.surfaceText : Theme.surfaceVariantText
                                        elide: Text.ElideRight
                                        maximumLineCount: 1
                                    }

                                    StyledText {
                                        width: parent.width
                                        text: mailRow.modelData.subject || "(sin asunto)"
                                        font.pixelSize: Theme.fontSizeSmall
                                        color: mailRow.modelData.unread ? Theme.surfaceText : Theme.surfaceVariantText
                                        elide: Text.ElideRight
                                        maximumLineCount: 1
                                    }
                                }

                                DankIcon {
                                    id: starMark
                                    visible: mailRow.modelData.starred === true
                                    name: "star"
                                    filled: true
                                    size: Theme.iconSizeSmall
                                    color: Theme.warning
                                    anchors.verticalCenter: parent.verticalCenter
                                }

                                StyledText {
                                    id: timeLabel
                                    text: root.timeOf(mailRow.modelData.lastMessageAt)
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: Theme.surfaceVariantText
                                    anchors.verticalCenter: parent.verticalCenter
                                }
                            }

                            // Hover actions: the notification set, per row.
                            Row {
                                id: rowActions
                                visible: rowHover.hovered
                                anchors.right: parent.right
                                anchors.rightMargin: Theme.spacingXS
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: 0

                                // Same order as dankmail's own triage row,
                                // followed by a focused quick-reply entry.
                                DankActionButton {
                                    iconName: "archive"
                                    buttonSize: 26
                                    iconSize: 15
                                    onClicked: root.op("ops.archive", mailRow.modelData.id)
                                }

                                DankActionButton {
                                    iconName: "delete"
                                    buttonSize: 26
                                    iconSize: 15
                                    iconColor: Theme.error
                                    onClicked: root.op("ops.trash", mailRow.modelData.id)
                                }

                                DankActionButton {
                                    iconName: mailRow.modelData.unread ? "drafts" : "mark_email_unread"
                                    buttonSize: 26
                                    iconSize: 15
                                    onClicked: root.op(mailRow.modelData.unread ? "ops.markRead" : "ops.markUnread", mailRow.modelData.id)
                                }

                                DankActionButton {
                                    iconName: "star"
                                    buttonSize: 26
                                    iconSize: 15
                                    iconColor: mailRow.modelData.starred ? Theme.warning : Theme.surfaceText
                                    onClicked: root.op(mailRow.modelData.starred ? "ops.unstar" : "ops.star", mailRow.modelData.id)
                                }

                                DankActionButton {
                                    iconName: "snooze"
                                    buttonSize: 26
                                    iconSize: 15
                                    onClicked: root.op("ops.snoozePreset", mailRow.modelData.id)
                                }

                                DankActionButton {
                                    iconName: "open_in_new"
                                    buttonSize: 26
                                    iconSize: 15
                                    onClicked: root.uiCall("ui.openLink", {
                                        "id": mailRow.modelData.id
                                    })
                                }

                                DankActionButton {
                                    iconName: "reply"
                                    buttonSize: 26
                                    iconSize: 15
                                    onClicked: {
                                        root.uiCall("ui.showThread", {
                                            "id": mailRow.modelData.id,
                                            "reply": true
                                        });
                                        if (popout.closePopout)
                                            popout.closePopout();
                                    }
                                }
                            }

                            // Click on the row body → open in the triage
                            // window (the action buttons' own MouseAreas
                            // take precedence over this handler).
                            TapHandler {
                                onTapped: {
                                    root.uiCall("ui.showThread", {
                                        "id": mailRow.modelData.id
                                    });
                                    if (popout.closePopout)
                                        popout.closePopout();
                                }
                            }
                        }
                    }
                    }
                }
            }
        }
    }

    verticalBarPill: Component {
        Item {
            implicitWidth: vCol.implicitWidth
            implicitHeight: root.pillHidden ? 0 : vCol.implicitHeight
            visible: !root.pillHidden

            // Middle click on the pill: toggle the app window (left
            // opens the popout, right syncs). Only MiddleButton is
            // accepted, so left/right fall through to BasePill.
            MouseArea {
                anchors.fill: parent
                // Cover BasePill's padding too — middle clicks on the
                // capsule margin were falling through to the bar canvas.
                anchors.margins: -10
                acceptedButtons: Qt.MiddleButton
                onClicked: root.toggleApp()
            }

            Column {
                id: vCol
                spacing: 1
                anchors.horizontalCenter: parent.horizontalCenter

                DankIcon {
                    name: root.syncing ? "sync" : (root.dnd ? "notifications_off" : "mail")
                    size: root.iconSize
                    color: {
                        if (!root.daemonConnected)
                            return Theme.surfaceVariantText;
                        return Theme.primary;
                    }
                    anchors.horizontalCenter: parent.horizontalCenter
                }

                NumericText {
                    visible: root.daemonConnected && root.unread > 0
                    text: root.unreadLabel
                    reserveText: "99+"
                    width: reservedWidth
                    font.pixelSize: Theme.fontSizeSmall
                    font.weight: Font.Bold
                    color: Theme.primary
                    horizontalAlignment: Text.AlignHCenter
                    anchors.horizontalCenter: parent.horizontalCenter
                }
            }
        }
    }
}
