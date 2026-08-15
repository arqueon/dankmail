import QtQuick
import Quickshell
import Quickshell.Io
import qs.Common
import qs.Services
import qs.Widgets
import qs.Modules.Plugins

PluginComponent {
    id: root

    layerNamespacePlugin: "dankmail-unread"

    // Settings
    property string dmailBin: pluginData.dmailBin || (Quickshell.env("HOME") + "/.local/bin/dmail")
    property int maxThreads: pluginData.maxThreads || 10

    // State
    property bool daemonUp: false
    property int unread: 0
    property bool dnd: false
    property var accounts: []
    property var threads: []

    readonly property string socketPath: (Quickshell.env("XDG_RUNTIME_DIR") || "/tmp") + "/dankmail.sock"

    // Subscribe stream: daemon presence + push events (unread.changed et al).
    Socket {
        id: eventSocket
        path: root.socketPath
        connected: true

        onConnectionStateChanged: {
            root.daemonUp = connected;
            if (connected) {
                write('{"id":1,"method":"subscribe"}\n');
                flush();
                root.refresh();
            } else {
                root.unread = 0;
                root.threads = [];
                retryTimer.restart();
            }
        }

        parser: SplitParser {
            onRead: line => {
                let msg;
                try {
                    msg = JSON.parse(line);
                } catch (e) {
                    return;
                }
                switch (msg.topic) {
                case "unread.changed":
                case "threads.changed":
                case "sync.updated":
                case "settings.changed":
                    refreshDebounce.restart();
                    break;
                }
            }
        }
    }

    Timer {
        id: retryTimer
        interval: 5000
        onTriggered: {
            eventSocket.connected = false;
            Qt.callLater(() => eventSocket.connected = true);
        }
    }

    Timer {
        id: refreshDebounce
        interval: 400
        onTriggered: root.refresh()
    }

    Process {
        id: statusProc
        command: [root.dmailBin, "status", "--json"]
        stdout: StdioCollector {
            onStreamFinished: {
                try {
                    const s = JSON.parse(text);
                    root.unread = s.unread || 0;
                    root.dnd = s.dnd === true;
                    root.accounts = s.accounts || [];
                } catch (e) {}
            }
        }
    }

    Process {
        id: listProc
        command: [root.dmailBin, "list", "--unread", "--json", "--limit", String(root.maxThreads)]
        stdout: StdioCollector {
            onStreamFinished: {
                try {
                    const t = JSON.parse(text);
                    root.threads = Array.isArray(t) ? t : (t.threads || []);
                } catch (e) {
                    root.threads = [];
                }
            }
        }
    }

    function refresh() {
        if (!daemonUp)
            return;
        statusProc.running = true;
        listProc.running = true;
    }

    function dmail(args) {
        Quickshell.execDetached([root.dmailBin].concat(args));
    }

    function relTime(iso) {
        const d = new Date(iso);
        if (isNaN(d))
            return "";
        const mins = Math.floor((Date.now() - d.getTime()) / 60000);
        if (mins < 1)
            return "now";
        if (mins < 60)
            return mins + "m";
        const hours = Math.floor(mins / 60);
        if (hours < 24)
            return hours + "h";
        return Math.floor(hours / 24) + "d";
    }

    pillRightClickAction: () => root.dmail(["toggle"])

    horizontalBarPill: Component {
        Row {
            spacing: Theme.spacingXS

            DankIcon {
                name: root.dnd ? "notifications_off" : "mail"
                size: Theme.iconSize - 6
                color: !root.daemonUp ? Theme.surfaceVariantText : (root.unread > 0 ? Theme.primary : (Theme.widgetIconColor || Theme.surfaceText))
                anchors.verticalCenter: parent.verticalCenter
            }

            StyledText {
                text: root.unread.toString()
                font.pixelSize: Theme.fontSizeMedium
                font.weight: Font.Medium
                color: Theme.primary
                visible: root.unread > 0
                anchors.verticalCenter: parent.verticalCenter
            }
        }
    }

    verticalBarPill: Component {
        Column {
            spacing: 2

            DankIcon {
                name: root.dnd ? "notifications_off" : "mail"
                size: 20
                color: !root.daemonUp ? Theme.surfaceVariantText : (root.unread > 0 ? Theme.primary : (Theme.widgetIconColor || Theme.surfaceText))
                anchors.horizontalCenter: parent.horizontalCenter
            }

            StyledText {
                text: root.unread.toString()
                font.pixelSize: Theme.fontSizeSmall
                color: Theme.surfaceText
                visible: root.unread > 0
                anchors.horizontalCenter: parent.horizontalCenter
            }
        }
    }

    popoutContent: Component {
        Column {
            width: parent.width
            spacing: Theme.spacingM
            topPadding: Theme.spacingM
            bottomPadding: Theme.spacingM

            // Header: title + actions
            Item {
                width: parent.width
                height: 36

                Row {
                    anchors.left: parent.left
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.spacingS

                    DankIcon {
                        name: "mail"
                        size: 22
                        color: Theme.primary
                        anchors.verticalCenter: parent.verticalCenter
                    }

                    StyledText {
                        text: "Dank Mail"
                        font.pixelSize: Theme.fontSizeLarge
                        font.weight: Font.Bold
                        color: Theme.surfaceText
                        anchors.verticalCenter: parent.verticalCenter
                    }

                    StyledText {
                        text: !root.daemonUp ? "daemon offline" : (root.accounts.length === 0 ? "no accounts" : root.unread + " unread")
                        font.pixelSize: Theme.fontSizeSmall
                        color: !root.daemonUp ? Theme.error : Theme.surfaceVariantText
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }

                Row {
                    anchors.right: parent.right
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.spacingXS

                    DankActionButton {
                        iconName: "sync"
                        buttonSize: 30
                        enabled: root.daemonUp
                        onClicked: root.dmail(["sync"])
                    }

                    DankActionButton {
                        iconName: root.dnd ? "notifications_off" : "notifications"
                        buttonSize: 30
                        enabled: root.daemonUp
                        onClicked: {
                            root.dmail(["dnd", root.dnd ? "off" : "on"]);
                            refreshDebounce.restart();
                        }
                    }

                    DankActionButton {
                        iconName: "open_in_new"
                        buttonSize: 30
                        onClicked: {
                            root.dmail(["show"]);
                            root.closePopout();
                        }
                    }
                }
            }

            // Empty / offline states
            StyledText {
                width: parent.width
                visible: !root.daemonUp
                text: "dankmail daemon is not running.\nStart it with: systemctl --user start dmail"
                font.pixelSize: Theme.fontSizeSmall
                color: Theme.surfaceVariantText
                wrapMode: Text.WordWrap
            }

            StyledText {
                width: parent.width
                visible: root.daemonUp && root.accounts.length === 0
                text: "No mail accounts configured yet.\nOpen the triage window and use the settings cog to add one."
                font.pixelSize: Theme.fontSizeSmall
                color: Theme.surfaceVariantText
                wrapMode: Text.WordWrap
            }

            StyledText {
                width: parent.width
                visible: root.daemonUp && root.accounts.length > 0 && root.threads.length === 0
                text: "Inbox zero. Nothing unread."
                font.pixelSize: Theme.fontSizeMedium
                color: Theme.surfaceVariantText
            }

            // Unread threads
            Column {
                width: parent.width
                spacing: Theme.spacingXS

                Repeater {
                    model: root.threads

                    delegate: Rectangle {
                        required property var modelData

                        width: parent.width
                        height: 56
                        radius: Theme.cornerRadius
                        color: threadArea.containsMouse ? Qt.rgba(Theme.primary.r, Theme.primary.g, Theme.primary.b, 0.12) : Qt.rgba(Theme.surfaceVariant.r, Theme.surfaceVariant.g, Theme.surfaceVariant.b, 0.3)

                        MouseArea {
                            id: threadArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                root.dmail(["open", String(modelData.id)]);
                                root.closePopout();
                            }
                        }

                        Column {
                            anchors.left: parent.left
                            anchors.right: timeText.left
                            anchors.leftMargin: Theme.spacingS
                            anchors.rightMargin: Theme.spacingS
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 2

                            StyledText {
                                width: parent.width
                                text: modelData.subject || "(no subject)"
                                font.pixelSize: Theme.fontSizeMedium
                                font.weight: Font.Medium
                                color: Theme.surfaceText
                                wrapMode: Text.NoWrap
                                maximumLineCount: 1
                                elide: Text.ElideRight
                            }

                            StyledText {
                                width: parent.width
                                text: (modelData.participants && modelData.participants.length > 0 ? modelData.participants[0] + " — " : "") + (modelData.snippet || "")
                                font.pixelSize: Theme.fontSizeSmall
                                color: Theme.surfaceVariantText
                                wrapMode: Text.NoWrap
                                maximumLineCount: 1
                                elide: Text.ElideRight
                            }
                        }

                        StyledText {
                            id: timeText
                            anchors.right: parent.right
                            anchors.rightMargin: Theme.spacingS
                            anchors.verticalCenter: parent.verticalCenter
                            text: root.relTime(modelData.lastMessageAt)
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.surfaceVariantText
                        }
                    }
                }
            }
        }
    }

    popoutWidth: 360
    popoutHeight: 0
}
