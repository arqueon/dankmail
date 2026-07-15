import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Common
import qs.Services
import qs.Widgets

// Settings: notification buttons, snooze default, chained-action
// policies, and account management. Everything applies live through
// the daemon (settings.set) — no restarts.
FloatingWindow {
    id: modal

    title: I18n.tr("Settings", "settings title")
    implicitWidth: 620
    implicitHeight: 640
    minimumSize: Qt.size(540, 480)
    color: Theme.surface
    visible: false

    signal addAccountRequested

    readonly property var s: DankMailService.settingsData

    function show() {
        DankMailService.refreshSettings();
        DankMailService.refreshAccounts();
        visible = true;
    }

    function actionEnabled(key) {
        const list = (s && s.notifyActions) ? s.notifyActions : [];
        return list.indexOf(key) !== -1;
    }

    function toggleAction(key) {
        let list = ((s && s.notifyActions) ? s.notifyActions : []).slice();
        const i = list.indexOf(key);
        if (i !== -1)
            list.splice(i, 1);
        else
            list.push(key);
        DankMailService.updateSettings({
            "notifyActions": list
        }, null);
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: Theme.spacingXL
        spacing: Theme.spacingL

        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spacingM

            DankIcon {
                name: "settings"
                size: Theme.iconSize
                color: Theme.primary
            }

            StyledText {
                Layout.fillWidth: true
                text: I18n.tr("Settings", "settings title")
                font.pixelSize: Theme.fontSizeLarge
                font.weight: Font.DemiBold
            }

            DankActionButton {
                iconName: "close"
                onClicked: modal.visible = false
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
            contentHeight: settingsColumn.implicitHeight
            clip: true

            ColumnLayout {
                id: settingsColumn
                width: parent.width
                spacing: Theme.spacingL

                // ---- notifications ------------------------------------
                StyledText {
                    text: I18n.tr("Notification buttons", "settings section")
                    font.pixelSize: Theme.fontSizeMedium
                    font.weight: Font.DemiBold
                    color: Theme.primary
                }

                StyledText {
                    Layout.fillWidth: true
                    text: I18n.tr("Pick the inline actions on new-mail notifications. Most notification servers show at most 3 buttons.", "settings")
                    font.pixelSize: Theme.fontSizeSmall
                    color: Theme.surfaceTextMedium
                    wrapMode: Text.WordWrap
                }

                Flow {
                    Layout.fillWidth: true
                    spacing: Theme.spacingS

                    // Same order as the per-thread action bar in the app.
                    Repeater {
                        model: [
                            {
                                "key": "archive",
                                "label": I18n.tr("Archive", "notify action")
                            },
                            {
                                "key": "trash",
                                "label": I18n.tr("Trash", "notify action")
                            },
                            {
                                "key": "read",
                                "label": I18n.tr("Mark read", "notify action")
                            },
                            {
                                "key": "snooze",
                                "label": I18n.tr("Snooze", "notify action")
                            },
                            {
                                "key": "open",
                                "label": I18n.tr("Open in web", "notify action")
                            }
                        ]

                        delegate: StyledRect {
                            id: actionChip
                            required property var modelData
                            readonly property bool active: modal.actionEnabled(modelData.key)

                            width: chipLabel.implicitWidth + Theme.spacingXL
                            height: 32
                            radius: 16
                            color: active ? Theme.primaryContainer : Theme.surfaceContainerHigh

                            StyledText {
                                id: chipLabel
                                anchors.centerIn: parent
                                text: actionChip.modelData.label
                                font.pixelSize: Theme.fontSizeSmall
                                color: actionChip.active ? Theme.primary : Theme.surfaceTextMedium
                            }

                            StateLayer {
                                stateColor: Theme.primary
                                onClicked: modal.toggleAction(actionChip.modelData.key)
                            }
                        }
                    }
                }

                StyledText {
                    Layout.fillWidth: true
                    text: I18n.tr("What the notification's snooze button does:", "settings")
                    font.pixelSize: Theme.fontSizeSmall
                    color: Theme.surfaceTextMedium
                }

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
                                "key": "laterweek",
                                "label": I18n.tr("Later this week", "snooze")
                            },
                            {
                                "key": "weekend",
                                "label": I18n.tr("This weekend", "snooze")
                            },
                            {
                                "key": "nextweek",
                                "label": I18n.tr("Next week", "snooze")
                            },
                            {
                                "key": "minutes",
                                "label": I18n.tr("Fixed minutes", "snooze")
                            }
                        ]

                        delegate: StyledRect {
                            id: presetChip
                            required property var modelData
                            readonly property bool active: modal.s && modal.s.snoozePreset === modelData.key

                            width: presetChipLabel.implicitWidth + Theme.spacingL
                            height: 30
                            radius: 15
                            color: active ? Theme.primaryContainer : Theme.surfaceContainerHigh

                            StyledText {
                                id: presetChipLabel
                                anchors.centerIn: parent
                                text: presetChip.modelData.label
                                font.pixelSize: Theme.fontSizeSmall
                                color: presetChip.active ? Theme.primary : Theme.surfaceTextMedium
                            }

                            StateLayer {
                                stateColor: Theme.primary
                                onClicked: DankMailService.updateSettings({
                                    "snoozePreset": presetChip.modelData.key
                                }, null)
                            }
                        }
                    }
                }

                RowLayout {
                    Layout.fillWidth: true
                    spacing: Theme.spacingM
                    visible: modal.s && modal.s.snoozePreset === "minutes"

                    StyledText {
                        text: I18n.tr("Notification snooze duration (minutes)", "settings")
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.surfaceTextMedium
                    }

                    DankTextField {
                        Layout.preferredWidth: 100
                        text: (modal.s && modal.s.snoozeMinutes) ? String(modal.s.snoozeMinutes) : "60"
                        onAccepted: {
                            const mins = parseInt(text);
                            if (mins > 0)
                                DankMailService.updateSettings({
                                    "snoozeMinutes": mins
                                }, null);
                        }
                    }
                }

                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 1
                    color: Theme.outlineLight
                }

                // ---- chained actions ----------------------------------
                StyledText {
                    text: I18n.tr("Chained actions", "settings section")
                    font.pixelSize: Theme.fontSizeMedium
                    font.weight: Font.DemiBold
                    color: Theme.primary
                }

                Repeater {
                    model: [
                        {
                            "key": "markReadOnPreview",
                            "label": I18n.tr("Opening the preview marks the thread read", "settings")
                        },
                        {
                            "key": "markReadOnReply",
                            "label": I18n.tr("Sending a reply marks the thread read", "settings")
                        },
                        {
                            "key": "markReadOnTrash",
                            "label": I18n.tr("Trashing also marks read", "settings")
                        },
                        {
                            "key": "unarchiveOnStar",
                            "label": I18n.tr("Starring moves the thread back to the inbox", "settings")
                        }
                    ]

                    delegate: RowLayout {
                        id: policyRow
                        required property var modelData
                        Layout.fillWidth: true
                        spacing: Theme.spacingM

                        StyledText {
                            Layout.fillWidth: true
                            text: policyRow.modelData.label
                            font.pixelSize: Theme.fontSizeSmall
                            wrapMode: Text.WordWrap
                        }

                        DankToggle {
                            checked: modal.s ? !!modal.s[policyRow.modelData.key] : false
                            onToggled: checked => {
                                const patch = {};
                                patch[policyRow.modelData.key] = checked;
                                DankMailService.updateSettings(patch, null);
                            }
                        }
                    }
                }

                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 1
                    color: Theme.outlineLight
                }

                // ---- sync interval ------------------------------------
                StyledText {
                    text: I18n.tr("Sync interval", "settings section")
                    font.pixelSize: Theme.fontSizeMedium
                    font.weight: Font.DemiBold
                    color: Theme.primary
                }

                StyledText {
                    Layout.fillWidth: true
                    text: I18n.tr("How often each account is polled for new mail. Shorter is snappier; all options stay well within provider rate limits.", "settings")
                    font.pixelSize: Theme.fontSizeSmall
                    color: Theme.surfaceTextMedium
                    wrapMode: Text.WordWrap
                }

                Flow {
                    Layout.fillWidth: true
                    spacing: Theme.spacingS

                    Repeater {
                        model: [
                            {
                                "value": 30,
                                "label": I18n.tr("30 seconds", "sync interval")
                            },
                            {
                                "value": 60,
                                "label": I18n.tr("1 minute", "sync interval")
                            },
                            {
                                "value": 120,
                                "label": I18n.tr("2 minutes", "sync interval")
                            },
                            {
                                "value": 300,
                                "label": I18n.tr("5 minutes", "sync interval")
                            },
                            {
                                "value": 900,
                                "label": I18n.tr("15 minutes", "sync interval")
                            }
                        ]

                        delegate: StyledRect {
                            id: intervalChip
                            required property var modelData
                            readonly property bool active: (modal.s ? (modal.s.pollSeconds || 60) : 60) === modelData.value

                            width: intervalChipLabel.implicitWidth + Theme.spacingL
                            height: 30
                            radius: 15
                            color: active ? Theme.primaryContainer : Theme.surfaceContainerHigh

                            StyledText {
                                id: intervalChipLabel
                                anchors.centerIn: parent
                                text: intervalChip.modelData.label
                                font.pixelSize: Theme.fontSizeSmall
                                color: intervalChip.active ? Theme.primary : Theme.surfaceTextMedium
                            }

                            StateLayer {
                                stateColor: Theme.primary
                                onClicked: DankMailService.updateSettings({
                                    "pollSeconds": intervalChip.modelData.value
                                }, null)
                            }
                        }
                    }
                }

                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 1
                    color: Theme.outlineLight
                }

                // ---- accounts -----------------------------------------
                RowLayout {
                    Layout.fillWidth: true

                    StyledText {
                        Layout.fillWidth: true
                        text: I18n.tr("Accounts", "settings section")
                        font.pixelSize: Theme.fontSizeMedium
                        font.weight: Font.DemiBold
                        color: Theme.primary
                    }

                    DankActionButton {
                        iconName: "person_add"
                        onClicked: {
                            modal.visible = false;
                            modal.addAccountRequested();
                        }
                    }
                }

                Repeater {
                    model: DankMailService.accounts

                    delegate: StyledRect {
                        id: acctRow
                        required property var modelData
                        property bool reauthPending: false
                        Layout.fillWidth: true
                        implicitHeight: 56
                        color: Theme.surfaceContainer

                        RowLayout {
                            anchors.fill: parent
                            anchors.margins: Theme.spacingM
                            spacing: Theme.spacingM

                            Rectangle {
                                width: 10
                                height: 10
                                radius: 5
                                color: DankMailService.accountColor(acctRow.modelData.id)
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: 0

                                StyledText {
                                    text: acctRow.modelData.email
                                    font.pixelSize: Theme.fontSizeSmall
                                    font.weight: Font.DemiBold
                                }

                                StyledText {
                                    text: acctRow.modelData.type + " · " + acctRow.modelData.status + (acctRow.modelData.unread > 0 ? " · " + acctRow.modelData.unread + " " + I18n.tr("unread", "tray tooltip") : "")
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: acctRow.modelData.status === "active" ? Theme.surfaceTextMedium : Theme.warning
                                }
                            }

                            // Re-run the OAuth consent with the stored client
                            // (Gmail only; IMAP re-auth means re-adding).
                            DankActionButton {
                                visible: acctRow.modelData.type === "gmail"
                                enabled: !acctRow.reauthPending
                                iconName: acctRow.reauthPending ? "hourglass_top" : "key"
                                iconColor: acctRow.modelData.status === "auth_error" ? Theme.warning : Theme.surfaceTextMedium
                                onClicked: {
                                    acctRow.reauthPending = true;
                                    DankMailService.reauthAccount(acctRow.modelData.id, err => {
                                        acctRow.reauthPending = false;
                                    });
                                }
                            }

                            DankActionButton {
                                iconName: "delete"
                                iconColor: Theme.error
                                onClicked: DankMailService.removeAccount(acctRow.modelData.id)
                            }
                        }
                    }
                }
            }
        }
    }
}
