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

    title: "Dank Mail"
    implicitWidth: 980
    implicitHeight: 620
    minimumSize: Qt.size(720, 420)
    color: Theme.surface

    property int selectedThreadId: -1

    function selectThread(t) {
        selectedThreadId = t.id;
        DankMailService.loadThread(t.id, true);
    }

    function selectedIds() {
        return selectedThreadId >= 0 ? [selectedThreadId] : [];
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

    AccountAddModal {
        id: accountModal
    }

    Shortcut {
        sequence: "Escape"
        onActivated: window.hideRequested()
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
                            }
                        ]

                        delegate: StyledRect {
                            required property var modelData
                            readonly property bool active: (modelData.key === "unread" && DankMailService.filterUnread) || (modelData.key === "starred" && DankMailService.filterStarred) || (modelData.key === "all" && !DankMailService.filterUnread && !DankMailService.filterStarred)

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
                                    DankMailService.refreshThreads();
                                }
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

                StyledText {
                    visible: !DankMailService.connected
                    text: I18n.tr("Daemon offline", "header status")
                    color: Theme.error
                    font.pixelSize: Theme.fontSizeSmall
                }

                DankActionButton {
                    iconName: "person_add"
                    onClicked: accountModal.show()
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
                            spacing: Theme.spacingS

                            Rectangle {
                                width: 8
                                height: 8
                                radius: 4
                                color: row.modelData.unread ? Theme.primary : "transparent"
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: 2

                                RowLayout {
                                    Layout.fillWidth: true

                                    StyledText {
                                        Layout.fillWidth: true
                                        text: (row.modelData.participants && row.modelData.participants.length > 0) ? row.modelData.participants[0] : ""
                                        font.weight: row.modelData.unread ? Font.DemiBold : Font.Normal
                                        font.pixelSize: Theme.fontSizeSmall
                                        maximumLineCount: 1
                                    }

                                    StyledText {
                                        text: window.timeLabel(row.modelData.lastMessageAt)
                                        font.pixelSize: Theme.fontSizeSmall
                                        color: Theme.surfaceTextMedium
                                    }
                                }

                                StyledText {
                                    Layout.fillWidth: true
                                    text: row.modelData.subject || I18n.tr("(no subject)", "thread list")
                                    font.weight: row.modelData.unread ? Font.DemiBold : Font.Normal
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

                        StyledText {
                            Layout.fillWidth: true
                            readonly property var msgs: DankMailService.currentThread ? (DankMailService.currentThread.messages || []) : []
                            text: {
                                if (msgs.length === 0)
                                    return "";
                                const last = msgs[msgs.length - 1];
                                return last.from + "  ·  " + Qt.formatDateTime(new Date(last.date), "ddd d MMM, HH:mm");
                            }
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.surfaceTextMedium
                        }
                    }

                    // Action bar.
                    RowLayout {
                        spacing: Theme.spacingXS

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
                            iconName: "snooze"
                            onClicked: snoozeMenu.visible = !snoozeMenu.visible
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

                    // Snooze options (inline popup).
                    StyledRect {
                        id: snoozeMenu
                        visible: false
                        Layout.fillWidth: true
                        implicitHeight: snoozeRow.implicitHeight + Theme.spacingM
                        color: Theme.surfaceContainerHigh

                        RowLayout {
                            id: snoozeRow
                            anchors.centerIn: parent
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
                                    required property var modelData
                                    width: snoozeLabel.implicitWidth + Theme.spacingL
                                    height: 30
                                    radius: 15
                                    color: Theme.surfaceContainerHighest

                                    StyledText {
                                        id: snoozeLabel
                                        anchors.centerIn: parent
                                        text: parent.modelData.label
                                        font.pixelSize: Theme.fontSizeSmall
                                    }

                                    StateLayer {
                                        stateColor: Theme.primary
                                        onClicked: {
                                            const ids = window.selectedIds();
                                            if (ids.length) {
                                                DankMailService.snooze(ids, window.snoozeUntil(parent.modelData.key));
                                                DankMailService.currentThread = null;
                                            }
                                            snoozeMenu.visible = false;
                                        }
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
                            textFormat: TextEdit.PlainText
                            color: Theme.surfaceText
                            selectionColor: Theme.primarySelected
                            font.family: Theme.fontFamily
                            font.pixelSize: Theme.fontSizeMedium
                            text: {
                                const t = DankMailService.currentThread;
                                if (!t || !t.messages || t.messages.length === 0)
                                    return "";
                                return t.messages[t.messages.length - 1].bodyText || "";
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
