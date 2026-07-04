import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Common
import qs.Services
import qs.Widgets

// Guided "add Gmail account" wizard, modeled on dankcalendar's
// AccountAddModal: the setup steps come from the daemon
// (accounts.gmail.setupGuide), then client credentials, then the OAuth
// consent in the browser (daemon-run loopback flow).
FloatingWindow {
    id: modal

    title: I18n.tr("Add account", "account wizard title")
    implicitWidth: 640
    implicitHeight: 560
    minimumSize: Qt.size(560, 480)
    color: Theme.surface
    visible: false

    property int wizardStep: 0
    property string clientId: ""
    property string clientSecret: ""
    property string pendingState: ""
    property string pendingAuthUrl: ""
    property string flowError: ""
    property bool flowInProgress: false
    property string completedEmail: ""

    readonly property var steps: DankMailService.gmailSetupSteps
    readonly property int guideCount: steps.length
    readonly property int credsStep: guideCount
    readonly property int browserStep: guideCount + 1
    readonly property int doneStep: guideCount + 2

    function show() {
        resetState();
        DankMailService.refreshGmailSetupSteps();
        visible = true;
    }

    function hide() {
        cancelPendingFlow();
        visible = false;
    }

    function resetState() {
        wizardStep = 0;
        clientId = DankMailService.gmailDefaultClientId;
        clientSecret = "";
        pendingState = "";
        pendingAuthUrl = "";
        flowError = "";
        flowInProgress = false;
        completedEmail = "";
    }

    function cancelPendingFlow() {
        if (pendingState !== "") {
            DankMailService.cancelFlow(pendingState);
            pendingState = "";
        }
        flowInProgress = false;
    }

    function startFlow() {
        flowError = "";
        flowInProgress = true;
        DankMailService.startGmailFlow(clientId.trim(), clientSecret.trim(), res => {
            if (res.error) {
                flowError = res.error;
                flowInProgress = false;
                return;
            }
            pendingState = res.state;
            pendingAuthUrl = res.authUrl;
            wizardStep = browserStep;
            Qt.openUrlExternally(res.authUrl);
            DankMailService.completeGmailFlow(res.state, done => {
                flowInProgress = false;
                if (done.error) {
                    flowError = done.error;
                    wizardStep = credsStep;
                    pendingState = "";
                    return;
                }
                completedEmail = done.email || "";
                pendingState = "";
                wizardStep = doneStep;
            });
        });
    }

    onVisibleChanged: {
        if (!visible)
            cancelPendingFlow();
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: Theme.spacingXL
        spacing: Theme.spacingL

        // Header.
        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spacingM

            DankIcon {
                name: "person_add"
                size: Theme.iconSize
                color: Theme.primary
            }

            StyledText {
                Layout.fillWidth: true
                text: {
                    if (modal.wizardStep < modal.guideCount)
                        return I18n.tr("Set up your Google OAuth client", "account wizard") + "  ·  " + (modal.wizardStep + 1) + "/" + modal.guideCount;
                    if (modal.wizardStep === modal.credsStep)
                        return I18n.tr("Enter your client credentials", "account wizard");
                    if (modal.wizardStep === modal.browserStep)
                        return I18n.tr("Authorize in the browser", "account wizard");
                    return I18n.tr("Account added", "account wizard");
                }
                font.pixelSize: Theme.fontSizeLarge
                font.weight: Font.DemiBold
            }

            DankActionButton {
                iconName: "close"
                onClicked: modal.hide()
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: Theme.outlineMedium
        }

        // Pages.
        Item {
            Layout.fillWidth: true
            Layout.fillHeight: true

            // ---- guide pages ---------------------------------------------
            ColumnLayout {
                id: guidePage
                anchors.fill: parent
                visible: modal.wizardStep < modal.guideCount && modal.guideCount > 0
                spacing: Theme.spacingL

                readonly property var step: (modal.wizardStep < modal.steps.length) ? modal.steps[modal.wizardStep] : null

                StyledText {
                    Layout.fillWidth: true
                    text: guidePage.step ? guidePage.step.title : ""
                    font.pixelSize: Theme.fontSizeXLarge
                    font.weight: Font.DemiBold
                }

                StyledText {
                    Layout.fillWidth: true
                    text: guidePage.step ? guidePage.step.description : ""
                    color: Theme.surfaceTextMedium
                    wrapMode: Text.WordWrap
                }

                StyledRect {
                    visible: guidePage.step !== null && guidePage.step.note !== undefined && guidePage.step.note !== ""
                    Layout.fillWidth: true
                    implicitHeight: noteText.implicitHeight + Theme.spacingL
                    color: Theme.primaryBackground
                    border.width: 1
                    border.color: Theme.outlineMedium

                    StyledText {
                        id: noteText
                        anchors.fill: parent
                        anchors.margins: Theme.spacingM
                        text: (guidePage.step && guidePage.step.note) ? guidePage.step.note : ""
                        font.pixelSize: Theme.fontSizeSmall
                        color: Theme.primary
                        wrapMode: Text.WordWrap
                    }
                }

                StyledRect {
                    visible: guidePage.step !== null && guidePage.step.url !== undefined && guidePage.step.url !== ""
                    width: linkRow.implicitWidth + Theme.spacingXL
                    height: 38
                    radius: 19
                    color: Theme.primaryContainer

                    RowLayout {
                        id: linkRow
                        anchors.centerIn: parent
                        spacing: Theme.spacingS

                        DankIcon {
                            name: "open_in_new"
                            size: Theme.iconSizeSmall
                            color: Theme.primary
                        }

                        StyledText {
                            text: (guidePage.step && guidePage.step.urlLabel) ? guidePage.step.urlLabel : ""
                            color: Theme.primary
                        }
                    }

                    StateLayer {
                        stateColor: Theme.primary
                        onClicked: {
                            if (guidePage.step && guidePage.step.url)
                                Qt.openUrlExternally(guidePage.step.url);
                        }
                    }
                }

                Item {
                    Layout.fillHeight: true
                }
            }

            // ---- credentials page ----------------------------------------
            ColumnLayout {
                anchors.fill: parent
                visible: modal.wizardStep === modal.credsStep
                spacing: Theme.spacingL

                StyledText {
                    Layout.fillWidth: true
                    text: I18n.tr("Paste the Client ID and Client Secret of the OAuth client you just created. They are stored in your system keyring, never in files.", "account wizard")
                    color: Theme.surfaceTextMedium
                    wrapMode: Text.WordWrap
                }

                DankTextField {
                    Layout.fillWidth: true
                    label: I18n.tr("Client ID", "account wizard")
                    text: modal.clientId
                    onTextChanged: modal.clientId = text
                }

                DankTextField {
                    Layout.fillWidth: true
                    label: I18n.tr("Client Secret", "account wizard")
                    text: modal.clientSecret
                    echoMode: TextInput.Password
                    onTextChanged: modal.clientSecret = text
                }

                StyledText {
                    visible: modal.flowError !== ""
                    Layout.fillWidth: true
                    text: modal.flowError
                    color: Theme.error
                    font.pixelSize: Theme.fontSizeSmall
                    wrapMode: Text.WordWrap
                }

                Item {
                    Layout.fillHeight: true
                }
            }

            // ---- browser wait page ---------------------------------------
            ColumnLayout {
                anchors.fill: parent
                visible: modal.wizardStep === modal.browserStep
                spacing: Theme.spacingL

                Item {
                    Layout.fillHeight: true
                }

                DankSpinner {
                    Layout.alignment: Qt.AlignHCenter
                }

                StyledText {
                    Layout.alignment: Qt.AlignHCenter
                    text: I18n.tr("Waiting for you to authorize dankmail in the browser…", "account wizard")
                    color: Theme.surfaceTextMedium
                }

                StyledRect {
                    Layout.alignment: Qt.AlignHCenter
                    width: reopenLabel.implicitWidth + Theme.spacingXL
                    height: 34
                    radius: 17
                    color: Theme.surfaceContainerHigh

                    StyledText {
                        id: reopenLabel
                        anchors.centerIn: parent
                        text: I18n.tr("Reopen consent page", "account wizard")
                        font.pixelSize: Theme.fontSizeSmall
                    }

                    StateLayer {
                        stateColor: Theme.primary
                        onClicked: {
                            if (modal.pendingAuthUrl !== "")
                                Qt.openUrlExternally(modal.pendingAuthUrl);
                        }
                    }
                }

                Item {
                    Layout.fillHeight: true
                }
            }

            // ---- done page ------------------------------------------------
            ColumnLayout {
                anchors.fill: parent
                visible: modal.wizardStep === modal.doneStep
                spacing: Theme.spacingL

                Item {
                    Layout.fillHeight: true
                }

                DankIcon {
                    Layout.alignment: Qt.AlignHCenter
                    name: "check_circle"
                    size: 56
                    color: Theme.success
                    filled: true
                }

                StyledText {
                    Layout.alignment: Qt.AlignHCenter
                    text: modal.completedEmail
                    font.pixelSize: Theme.fontSizeLarge
                    font.weight: Font.DemiBold
                }

                StyledText {
                    Layout.alignment: Qt.AlignHCenter
                    text: I18n.tr("Syncing has started. New mail will appear shortly.", "account wizard")
                    color: Theme.surfaceTextMedium
                }

                Item {
                    Layout.fillHeight: true
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: Theme.outlineMedium
        }

        // Footer navigation.
        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spacingM

            StyledRect {
                visible: modal.wizardStep > 0 && modal.wizardStep <= modal.credsStep
                width: backLabel.implicitWidth + Theme.spacingXL
                height: 36
                radius: 18
                color: Theme.surfaceContainerHigh

                StyledText {
                    id: backLabel
                    anchors.centerIn: parent
                    text: I18n.tr("Back", "account wizard nav")
                }

                StateLayer {
                    stateColor: Theme.primary
                    onClicked: modal.wizardStep = modal.wizardStep - 1
                }
            }

            StyledText {
                visible: modal.wizardStep < modal.guideCount
                text: I18n.tr("Already have a client? Skip the guide.", "account wizard nav")
                color: Theme.primary
                font.pixelSize: Theme.fontSizeSmall

                MouseArea {
                    anchors.fill: parent
                    cursorShape: Qt.PointingHandCursor
                    onClicked: modal.wizardStep = modal.credsStep
                }
            }

            Item {
                Layout.fillWidth: true
            }

            StyledRect {
                visible: modal.wizardStep < modal.credsStep
                width: nextLabel.implicitWidth + Theme.spacingXL
                height: 36
                radius: 18
                color: Theme.primaryContainer

                StyledText {
                    id: nextLabel
                    anchors.centerIn: parent
                    text: I18n.tr("Next", "account wizard nav")
                    color: Theme.primary
                }

                StateLayer {
                    stateColor: Theme.primary
                    onClicked: modal.wizardStep = modal.wizardStep + 1
                }
            }

            StyledRect {
                visible: modal.wizardStep === modal.credsStep
                readonly property bool ready: modal.clientId.trim() !== "" && modal.clientSecret.trim() !== "" && !modal.flowInProgress
                width: authLabel.implicitWidth + Theme.spacingXL
                height: 36
                radius: 18
                color: ready ? Theme.primaryContainer : Theme.surfaceContainerHigh
                opacity: ready ? 1 : 0.6

                StyledText {
                    id: authLabel
                    anchors.centerIn: parent
                    text: I18n.tr("Authorize", "account wizard nav")
                    color: parent.ready ? Theme.primary : Theme.surfaceTextMedium
                }

                StateLayer {
                    disabled: !parent.ready
                    stateColor: Theme.primary
                    onClicked: modal.startFlow()
                }
            }

            StyledRect {
                visible: modal.wizardStep === modal.browserStep
                width: cancelLabel.implicitWidth + Theme.spacingXL
                height: 36
                radius: 18
                color: Theme.surfaceContainerHigh

                StyledText {
                    id: cancelLabel
                    anchors.centerIn: parent
                    text: I18n.tr("Cancel", "account wizard nav")
                }

                StateLayer {
                    stateColor: Theme.error
                    onClicked: {
                        modal.cancelPendingFlow();
                        modal.wizardStep = modal.credsStep;
                    }
                }
            }

            StyledRect {
                visible: modal.wizardStep === modal.doneStep
                width: doneLabel.implicitWidth + Theme.spacingXL
                height: 36
                radius: 18
                color: Theme.primaryContainer

                StyledText {
                    id: doneLabel
                    anchors.centerIn: parent
                    text: I18n.tr("Done", "account wizard nav")
                    color: Theme.primary
                }

                StateLayer {
                    stateColor: Theme.primary
                    onClicked: modal.hide()
                }
            }
        }
    }
}
