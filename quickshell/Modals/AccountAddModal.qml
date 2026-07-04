import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Common
import qs.Services
import qs.Widgets

// Guided "add account" wizard, modeled on dankcalendar's
// AccountAddModal. First a provider chooser (Gmail / generic IMAP),
// then the provider-specific flow:
//   gmail — setup guide served by the daemon, client credentials, OAuth
//           consent in the browser (daemon-run loopback flow);
//   imap  — provider presets (iCloud, Outlook, Yahoo, Fastmail, Proton
//           Bridge, custom) + credentials; the daemon tests the
//           connection before storing anything.
FloatingWindow {
    id: modal

    title: I18n.tr("Add account", "account wizard title")
    implicitWidth: 640
    implicitHeight: 580
    minimumSize: Qt.size(560, 500)
    color: Theme.surface
    visible: false

    property string selectedProvider: "" // "" | "gmail" | "imap"
    property int wizardStep: 0
    property string flowError: ""
    property bool flowInProgress: false
    property string completedEmail: ""

    // Gmail flow state.
    property string clientId: ""
    property string clientSecret: ""
    property string pendingState: ""
    property string pendingAuthUrl: ""

    // IMAP form state.
    property string presetKey: ""
    property string presetNote: ""
    property string presetNoteUrl: ""
    property string imapEmail: ""
    property string imapPassword: ""
    property string imapHost: ""
    property string imapPort: "993"
    property string imapSecurity: "tls"
    property string imapSmtpHost: ""
    property string imapSmtpPort: "587"
    property string imapWebmail: ""

    readonly property var steps: DankMailService.gmailSetupSteps
    readonly property int guideCount: steps.length
    readonly property int credsStep: guideCount
    readonly property int browserStep: guideCount + 1
    readonly property int gmailDoneStep: guideCount + 2

    readonly property bool onProviderSelect: selectedProvider === ""
    readonly property bool onGmailGuide: selectedProvider === "gmail" && wizardStep < guideCount && guideCount > 0
    readonly property bool onGmailCreds: selectedProvider === "gmail" && wizardStep === credsStep
    readonly property bool onGmailBrowser: selectedProvider === "gmail" && wizardStep === browserStep
    readonly property bool onImapForm: selectedProvider === "imap" && wizardStep === 0
    readonly property bool onDone: (selectedProvider === "gmail" && wizardStep === gmailDoneStep) || (selectedProvider === "imap" && wizardStep === 1)

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
        selectedProvider = "";
        wizardStep = 0;
        flowError = "";
        flowInProgress = false;
        completedEmail = "";
        clientId = DankMailService.gmailDefaultClientId;
        clientSecret = "";
        pendingState = "";
        pendingAuthUrl = "";
        presetKey = "";
        presetNote = "";
        presetNoteUrl = "";
        imapEmail = "";
        imapPassword = "";
        imapHost = "";
        imapPort = "993";
        imapSecurity = "tls";
        imapSmtpHost = "";
        imapSmtpPort = "587";
        imapWebmail = "";
    }

    function cancelPendingFlow() {
        if (pendingState !== "") {
            DankMailService.cancelFlow(pendingState);
            pendingState = "";
        }
        flowInProgress = false;
    }

    function goBack() {
        flowError = "";
        if (wizardStep > 0) {
            wizardStep = wizardStep - 1;
        } else {
            selectedProvider = "";
        }
    }

    function applyPreset(p) {
        presetKey = p.key;
        presetNote = p.note || "";
        presetNoteUrl = p.noteUrl || "";
        imapHost = p.host || "";
        imapPort = p.port ? String(p.port) : "993";
        imapSecurity = p.security || "tls";
        imapSmtpHost = p.smtpHost || "";
        imapSmtpPort = p.smtpPort ? String(p.smtpPort) : "587";
        imapWebmail = p.webmailUrl || "";
    }

    function startGmailFlow() {
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
                wizardStep = gmailDoneStep;
            });
        });
    }

    function submitImap() {
        flowError = "";
        flowInProgress = true;
        DankMailService.addImapAccount({
            "email": imapEmail.trim(),
            "password": imapPassword,
            "host": imapHost.trim(),
            "port": parseInt(imapPort) || 0,
            "security": imapSecurity,
            "smtpHost": imapSmtpHost.trim(),
            "smtpPort": parseInt(imapSmtpPort) || 0,
            "webmailUrl": imapWebmail.trim()
        }, res => {
            flowInProgress = false;
            if (res.error) {
                flowError = res.error;
                return;
            }
            completedEmail = res.email || "";
            wizardStep = 1;
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
                    if (modal.onProviderSelect)
                        return I18n.tr("Choose your mail provider", "account wizard");
                    if (modal.onGmailGuide)
                        return I18n.tr("Set up your Google OAuth client", "account wizard") + "  ·  " + (modal.wizardStep + 1) + "/" + modal.guideCount;
                    if (modal.onGmailCreds)
                        return I18n.tr("Enter your client credentials", "account wizard");
                    if (modal.onGmailBrowser)
                        return I18n.tr("Authorize in the browser", "account wizard");
                    if (modal.onImapForm)
                        return I18n.tr("IMAP account", "account wizard");
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

            // ---- provider chooser ----------------------------------------
            ColumnLayout {
                anchors.fill: parent
                visible: modal.onProviderSelect
                spacing: Theme.spacingL

                Repeater {
                    model: [
                        {
                            "key": "gmail",
                            "icon": "mail",
                            "title": "Gmail",
                            "desc": I18n.tr("Official API: full triage, minimal scopes, guided OAuth setup.", "provider chooser")
                        },
                        {
                            "key": "imap",
                            "icon": "dns",
                            "title": I18n.tr("Other provider (IMAP)", "provider chooser"),
                            "desc": I18n.tr("iCloud, Outlook, Yahoo, Fastmail, Proton Bridge or your own server. Stored and verified now; syncing arrives with the IMAP ring.", "provider chooser")
                        }
                    ]

                    delegate: StyledRect {
                        id: provCard
                        required property var modelData
                        Layout.fillWidth: true
                        implicitHeight: 84
                        color: Theme.surfaceContainer
                        border.width: 1
                        border.color: Theme.outlineMedium

                        RowLayout {
                            anchors.fill: parent
                            anchors.margins: Theme.spacingL
                            spacing: Theme.spacingL

                            DankIcon {
                                name: provCard.modelData.icon
                                size: Theme.iconSizeLarge
                                color: Theme.primary
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: 2

                                StyledText {
                                    text: provCard.modelData.title
                                    font.pixelSize: Theme.fontSizeLarge
                                    font.weight: Font.DemiBold
                                }

                                StyledText {
                                    Layout.fillWidth: true
                                    text: provCard.modelData.desc
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: Theme.surfaceTextMedium
                                    wrapMode: Text.WordWrap
                                }
                            }

                            DankIcon {
                                name: "chevron_right"
                                color: Theme.surfaceTextAlpha
                            }
                        }

                        StateLayer {
                            stateColor: Theme.primary
                            onClicked: {
                                modal.selectedProvider = provCard.modelData.key;
                                modal.wizardStep = 0;
                                modal.flowError = "";
                            }
                        }
                    }
                }

                Item {
                    Layout.fillHeight: true
                }
            }

            // ---- gmail: guide pages --------------------------------------
            ColumnLayout {
                id: guidePage
                anchors.fill: parent
                visible: modal.onGmailGuide
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

            // ---- gmail: credentials page ---------------------------------
            ColumnLayout {
                anchors.fill: parent
                visible: modal.onGmailCreds
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

            // ---- gmail: browser wait page --------------------------------
            ColumnLayout {
                anchors.fill: parent
                visible: modal.onGmailBrowser
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

            // ---- imap: preset + form page --------------------------------
            DankFlickable {
                anchors.fill: parent
                visible: modal.onImapForm
                contentHeight: imapColumn.implicitHeight
                clip: true

                ColumnLayout {
                    id: imapColumn
                    width: parent.width
                    spacing: Theme.spacingM

                    StyledText {
                        Layout.fillWidth: true
                        text: I18n.tr("Pick your provider or configure a custom server. The connection is tested before anything is saved; the password goes to your system keyring.", "account wizard")
                        color: Theme.surfaceTextMedium
                        wrapMode: Text.WordWrap
                    }

                    Flow {
                        Layout.fillWidth: true
                        spacing: Theme.spacingS

                        Repeater {
                            model: DankMailService.imapPresets

                            delegate: StyledRect {
                                required property var modelData
                                readonly property bool active: modal.presetKey === modelData.key

                                width: presetLabel.implicitWidth + Theme.spacingL
                                height: 30
                                radius: 15
                                color: active ? Theme.primaryContainer : Theme.surfaceContainerHigh

                                StyledText {
                                    id: presetLabel
                                    anchors.centerIn: parent
                                    text: parent.modelData.label
                                    font.pixelSize: Theme.fontSizeSmall
                                    color: parent.active ? Theme.primary : Theme.surfaceText
                                }

                                StateLayer {
                                    stateColor: Theme.primary
                                    onClicked: modal.applyPreset(parent.modelData)
                                }
                            }
                        }
                    }

                    StyledRect {
                        visible: modal.presetNote !== ""
                        Layout.fillWidth: true
                        implicitHeight: presetNoteText.implicitHeight + Theme.spacingL
                        color: Theme.primaryBackground
                        border.width: 1
                        border.color: Theme.outlineMedium

                        StyledText {
                            id: presetNoteText
                            anchors.fill: parent
                            anchors.margins: Theme.spacingM
                            text: modal.presetNote + (modal.presetNoteUrl !== "" ? "  ↗" : "")
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.primary
                            wrapMode: Text.WordWrap
                        }

                        StateLayer {
                            visible: modal.presetNoteUrl !== ""
                            stateColor: Theme.primary
                            onClicked: Qt.openUrlExternally(modal.presetNoteUrl)
                        }
                    }

                    DankTextField {
                        Layout.fillWidth: true
                        label: I18n.tr("Email address", "account wizard")
                        text: modal.imapEmail
                        onTextChanged: modal.imapEmail = text
                    }

                    DankTextField {
                        Layout.fillWidth: true
                        label: I18n.tr("Password (or app password)", "account wizard")
                        text: modal.imapPassword
                        echoMode: TextInput.Password
                        onTextChanged: modal.imapPassword = text
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.spacingM

                        DankTextField {
                            Layout.fillWidth: true
                            label: I18n.tr("IMAP server", "account wizard")
                            text: modal.imapHost
                            onTextChanged: modal.imapHost = text
                        }

                        DankTextField {
                            Layout.preferredWidth: 110
                            label: I18n.tr("Port", "account wizard")
                            text: modal.imapPort
                            onTextChanged: modal.imapPort = text
                        }

                        StyledRect {
                            Layout.preferredWidth: securityLabel.implicitWidth + Theme.spacingL
                            Layout.preferredHeight: 44
                            Layout.alignment: Qt.AlignBottom
                            radius: Theme.cornerRadiusSmall
                            color: Theme.surfaceContainerHigh

                            StyledText {
                                id: securityLabel
                                anchors.centerIn: parent
                                text: modal.imapSecurity.toUpperCase()
                                font.pixelSize: Theme.fontSizeSmall
                            }

                            StateLayer {
                                stateColor: Theme.primary
                                onClicked: modal.imapSecurity = modal.imapSecurity === "tls" ? "starttls" : "tls"
                            }
                        }
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.spacingM

                        DankTextField {
                            Layout.fillWidth: true
                            label: I18n.tr("SMTP server (for replies, next ring)", "account wizard")
                            text: modal.imapSmtpHost
                            onTextChanged: modal.imapSmtpHost = text
                        }

                        DankTextField {
                            Layout.preferredWidth: 110
                            label: I18n.tr("Port", "account wizard")
                            text: modal.imapSmtpPort
                            onTextChanged: modal.imapSmtpPort = text
                        }
                    }

                    StyledText {
                        visible: modal.flowError !== ""
                        Layout.fillWidth: true
                        text: modal.flowError
                        color: Theme.error
                        font.pixelSize: Theme.fontSizeSmall
                        wrapMode: Text.WordWrap
                    }
                }
            }

            // ---- done page ------------------------------------------------
            ColumnLayout {
                anchors.fill: parent
                visible: modal.onDone
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
                    Layout.maximumWidth: parent.width - Theme.spacingXL * 2
                    horizontalAlignment: Text.AlignHCenter
                    text: modal.selectedProvider === "imap" ? I18n.tr("Connection verified and account stored. IMAP syncing arrives with the next ring; the account stays parked until then.", "account wizard") : I18n.tr("Syncing has started. New mail will appear shortly.", "account wizard")
                    color: Theme.surfaceTextMedium
                    wrapMode: Text.WordWrap
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
                visible: !modal.onProviderSelect && !modal.onDone && !modal.onGmailBrowser
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
                    onClicked: modal.goBack()
                }
            }

            StyledText {
                visible: modal.onGmailGuide
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

            DankSpinner {
                visible: modal.flowInProgress && modal.onImapForm
            }

            StyledRect {
                visible: modal.onGmailGuide
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
                visible: modal.onGmailCreds
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
                    onClicked: modal.startGmailFlow()
                }
            }

            StyledRect {
                visible: modal.onImapForm
                readonly property bool ready: modal.imapEmail.trim() !== "" && modal.imapPassword !== "" && modal.imapHost.trim() !== "" && !modal.flowInProgress
                width: addLabel.implicitWidth + Theme.spacingXL
                height: 36
                radius: 18
                color: ready ? Theme.primaryContainer : Theme.surfaceContainerHigh
                opacity: ready ? 1 : 0.6

                StyledText {
                    id: addLabel
                    anchors.centerIn: parent
                    text: modal.flowInProgress ? I18n.tr("Testing connection…", "account wizard nav") : I18n.tr("Add account", "account wizard nav")
                    color: parent.ready ? Theme.primary : Theme.surfaceTextMedium
                }

                StateLayer {
                    disabled: !parent.ready
                    stateColor: Theme.primary
                    onClicked: modal.submitImap()
                }
            }

            StyledRect {
                visible: modal.onGmailBrowser
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
                visible: modal.onDone
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
