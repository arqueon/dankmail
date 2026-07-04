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

                    Repeater {
                        model: [
                            {
                                "key": "read",
                                "label": I18n.tr("Mark read", "notify action")
                            },
                            {
                                "key": "archive",
                                "label": I18n.tr("Archive", "notify action")
                            },
                            {
                                "key": "trash",
                                "label": I18n.tr("Trash", "notify action")
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

                RowLayout {
                    Layout.fillWidth: true
                    spacing: Theme.spacingM

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
