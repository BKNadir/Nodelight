import QtQuick 2.9
import QtQuick.Controls 2.2
import QtQuick.Layouts 1.2

// Tailscale settings shown inside Moonlight. All state lives in NodelightAgent
// (window.nl); this page only displays it and sends the user's choices.
Flickable {
    id: tsPage
    objectName: qsTr("Tailscale")

    property var st: window.nl.status
    property bool connected: st !== null && (st.state === "running" || st.state === "starting" || st.state === "needs-login")
    property string exitKey: ""
    property string hostsKey: ""

    boundsBehavior: Flickable.OvershootBounds
    contentWidth: column.width
    contentHeight: column.height + 40
    clip: true

    ScrollBar.vertical: ScrollBar {
        anchors {
            left: parent.right
            leftMargin: -10
        }
    }

    function summary() {
        if (st === null) {
            return qsTr("Waiting for Nodelight...")
        }
        if (st.state === "running") {
            return qsTr("Connected") + " (" + st.tailscaleIp + ")"
        }
        if (st.state === "starting") {
            return qsTr("Connecting...")
        }
        if (st.state === "needs-login") {
            return qsTr("Sign-in required")
        }
        if (st.state === "error") {
            return qsTr("Error")
        }
        return qsTr("Disconnected")
    }

    // Refill the exit node list and the PC table only when the data changed
    // on the Nodelight side, so typing and choosing are not interrupted.
    onStChanged: {
        if (st === null) {
            return
        }

        var key = JSON.stringify(st.exitNodes) + "|" + st.exitNode
        if (key !== exitKey && !exitCombo.popup.visible) {
            exitKey = key
            exitModel.clear()
            exitModel.append({ "text": qsTr("None"), "val": "" })
            var selected = 0
            for (var i = 0; i < st.exitNodes.length; i++) {
                var node = st.exitNodes[i]
                exitModel.append({ "text": node.name + " (" + node.ip + ")", "val": node.ip })
                if (node.ip === st.exitNode || node.name.toLowerCase() === st.exitNode.toLowerCase()) {
                    selected = i + 1
                }
            }
            if (st.exitNode !== "" && selected === 0) {
                exitModel.append({ "text": st.exitNode, "val": st.exitNode })
                selected = exitModel.count - 1
            }
            exitCombo.currentIndex = selected
            exitCombo.recalculateWidth()
        }

        var hk = st.profile + "|" + JSON.stringify(st.hosts.map(function(h) { return [h.name, h.address, h.basePort] }))
        if (hk !== hostsKey) {
            hostsKey = hk
            hostModel.clear()
            for (var j = 0; j < st.hosts.length; j++) {
                var h = st.hosts[j]
                hostModel.append({ "hname": h.name, "haddr": h.address, "hport": h.basePort ? String(h.basePort) : "" })
            }
        }

        var names = []
        for (var k = 0; k < st.profiles.length; k++) {
            names.push(st.profiles[k])
        }
        if (JSON.stringify(names) !== profileKey) {
            profileKey = JSON.stringify(names)
            profileModel.clear()
            var sel = 0
            for (var m = 0; m < names.length; m++) {
                profileModel.append({ "text": names[m] })
                if (names[m] === st.profile) {
                    sel = m
                }
            }
            profileCombo.currentIndex = sel
            profileCombo.recalculateWidth()
        }
    }
    property string profileKey: ""

    Component.onCompleted: onStChanged()

    function hostsPayload() {
        var out = []
        for (var i = 0; i < hostModel.count; i++) {
            var h = hostModel.get(i)
            var port = parseInt(h.hport, 10)
            var spec = { "name": h.hname.trim(), "address": h.haddr.trim() }
            if (!isNaN(port) && port > 0) {
                spec.basePort = port
            }
            out.push(spec)
        }
        return { "hosts": out }
    }

    ColumnLayout {
        id: column
        width: Math.min(tsPage.width - 40, 720)
        x: 20
        y: 20
        spacing: 12

        Label {
            Layout.fillWidth: true
            text: qsTr("Nodelight is not running: start Moonlight with Nodelight.exe to use Tailscale.")
            wrapMode: Text.Wrap
            color: "orange"
            visible: !window.nl.available
        }

        GroupBox {
            Layout.fillWidth: true
            enabled: window.nl.available
            title: "<font color=\"skyblue\">" + qsTr("Connection") + "</font>"
            font.pointSize: 12

            ColumnLayout {
                anchors.fill: parent
                spacing: 6

                Label {
                    Layout.fillWidth: true
                    text: tsPage.summary()
                    font.pointSize: 14
                    wrapMode: Text.Wrap
                }

                Label {
                    Layout.fillWidth: true
                    text: window.nl.problem
                    wrapMode: Text.Wrap
                    color: "orange"
                    visible: window.nl.problem !== ""
                }

                Label {
                    Layout.fillWidth: true
                    text: qsTr("Profile: %1").arg(tsPage.st ? tsPage.st.profile : "")
                    visible: tsPage.st !== null && tsPage.st.profiles.length > 1
                }

                RowLayout {
                    spacing: 6

                    Button {
                        text: tsPage.connected ? qsTr("Disconnect") : qsTr("Connect")
                        enabled: !window.nl.busy && tsPage.st !== null
                        onClicked: window.nl.send("POST", tsPage.connected ? "/api/disconnect" : "/api/connect", null)
                    }

                    Button {
                        text: qsTr("Open sign-in page")
                        visible: tsPage.st !== null && !!tsPage.st.authUrl
                        onClicked: Qt.openUrlExternally(tsPage.st.authUrl)
                    }
                }
            }
        }

        GroupBox {
            Layout.fillWidth: true
            enabled: window.nl.available
            title: "<font color=\"skyblue\">" + qsTr("Exit node") + "</font>"
            font.pointSize: 12

            ColumnLayout {
                anchors.fill: parent
                spacing: 6

                Label {
                    Layout.fillWidth: true
                    text: qsTr("Needed when the PC is not on Tailscale but on the same network as a machine that is (address the PC by its local IP).")
                    wrapMode: Text.Wrap
                    font.pointSize: 10
                }

                AutoResizingComboBox {
                    id: exitCombo
                    textRole: "text"
                    model: ListModel {
                        id: exitModel
                    }
                }

                Button {
                    text: qsTr("Apply exit node")
                    enabled: !window.nl.busy && exitModel.count > 0
                    onClicked: {
                        var choice = exitModel.get(exitCombo.currentIndex)
                        tsPage.exitKey = ""
                        window.nl.send("POST", "/api/exit-node", { "exitNode": choice ? choice.val : "" })
                    }
                }
            }
        }

        GroupBox {
            Layout.fillWidth: true
            enabled: window.nl.available
            title: "<font color=\"skyblue\">" + qsTr("Your PCs (Sunshine / Apollo)") + "</font>"
            font.pointSize: 12

            ColumnLayout {
                anchors.fill: parent
                spacing: 6

                Label {
                    Layout.fillWidth: true
                    text: qsTr("Address: Tailscale name or IP, or the local IP of the PC when an exit node is used. Base port: leave empty for 47989.")
                    wrapMode: Text.Wrap
                    font.pointSize: 10
                }

                ListModel {
                    id: hostModel
                }

                Repeater {
                    model: hostModel

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: 6

                        Rectangle {
                            width: 12
                            height: 12
                            radius: 6
                            color: (tsPage.st && index < tsPage.st.hosts.length && tsPage.st.hosts[index].reachable) ? "limegreen" : "gray"
                        }

                        TextField {
                            Layout.preferredWidth: 130
                            placeholderText: qsTr("Name")
                            text: hname
                            onTextEdited: hostModel.setProperty(index, "hname", text)
                        }

                        TextField {
                            Layout.fillWidth: true
                            placeholderText: qsTr("Address")
                            text: haddr
                            onTextEdited: hostModel.setProperty(index, "haddr", text)
                        }

                        TextField {
                            Layout.preferredWidth: 80
                            placeholderText: "47989"
                            text: hport
                            inputMethodHints: Qt.ImhDigitsOnly
                            onTextEdited: hostModel.setProperty(index, "hport", text)
                        }

                        Label {
                            Layout.preferredWidth: 90
                            text: (tsPage.st && index < tsPage.st.hosts.length) ? tsPage.st.hosts[index].loopback : ""
                            font.pointSize: 9
                            opacity: 0.7
                        }

                        Button {
                            text: "✕"
                            onClicked: hostModel.remove(index)
                        }
                    }
                }

                RowLayout {
                    spacing: 6

                    Button {
                        text: qsTr("Add a PC")
                        onClicked: hostModel.append({ "hname": "", "haddr": "", "hport": "" })
                    }

                    Button {
                        text: qsTr("Save PCs")
                        enabled: !window.nl.busy
                        onClicked: {
                            tsPage.hostsKey = ""
                            window.nl.send("PUT", "/api/hosts", tsPage.hostsPayload())
                        }
                    }
                }
            }
        }

        GroupBox {
            Layout.fillWidth: true
            enabled: window.nl.available
            title: "<font color=\"skyblue\">" + qsTr("Profiles (one per tailnet)") + "</font>"
            font.pointSize: 12

            ColumnLayout {
                anchors.fill: parent
                spacing: 6

                AutoResizingComboBox {
                    id: profileCombo
                    textRole: "text"
                    model: ListModel {
                        id: profileModel
                    }
                }

                RowLayout {
                    spacing: 6

                    Button {
                        text: qsTr("Switch to this profile")
                        enabled: !window.nl.busy && profileModel.count > 0
                        onClicked: {
                            var p = profileModel.get(profileCombo.currentIndex)
                            tsPage.hostsKey = ""
                            tsPage.exitKey = ""
                            window.nl.send("POST", "/api/profile/select", { "name": p.text })
                        }
                    }

                    Button {
                        text: qsTr("Delete profile")
                        enabled: !window.nl.busy && profileModel.count > 1
                        onClicked: {
                            var p = profileModel.get(profileCombo.currentIndex)
                            window.nl.send("POST", "/api/profile/delete", { "name": p.text })
                        }
                    }
                }

                RowLayout {
                    spacing: 6

                    TextField {
                        id: newProfileName
                        Layout.preferredWidth: 160
                        placeholderText: qsTr("New profile name")
                    }

                    TextField {
                        id: newProfileServer
                        Layout.fillWidth: true
                        placeholderText: qsTr("Headscale URL (optional)")
                    }

                    Button {
                        text: qsTr("Create")
                        enabled: !window.nl.busy && newProfileName.text.trim() !== ""
                        onClicked: {
                            window.nl.send("POST", "/api/profile/create",
                                           { "name": newProfileName.text.trim(), "controlUrl": newProfileServer.text.trim() })
                            newProfileName.clear()
                            newProfileServer.clear()
                        }
                    }
                }
            }
        }
    }
}
