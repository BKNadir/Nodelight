import QtQuick 2.9

import ComputerManager 1.0

// Talks to the local Nodelight control API (address and secret passed by
// Nodelight.exe through the environment). It keeps the latest status, adds the
// Nodelight loopback PCs to Moonlight once the tunnel is up, and asks for the
// Tailscale page on first run.
Item {
    id: agent

    readonly property bool available: nodelightApi !== ""

    property var status: null
    property bool busy: false
    property string problem: ""
    property var added: ({})
    property bool firstRunHandled: false

    signal openTailscalePage()

    function call(method, path, body, callback) {
        busy = true
        var xhr = new XMLHttpRequest()
        xhr.onreadystatechange = function() {
            if (xhr.readyState !== 4) {
                return
            }
            busy = false
            var data = null
            try {
                data = JSON.parse(xhr.responseText)
            } catch (e) {
                data = null
            }
            callback(xhr.status, data)
        }
        xhr.open(method, nodelightApi + path)
        xhr.setRequestHeader("X-Nodelight-Token", nodelightToken)
        xhr.setRequestHeader("Content-Type", "application/json")
        xhr.send(body === null ? "" : JSON.stringify(body))
    }

    function apply(data) {
        status = data
        if (data.error) {
            problem = data.error
        } else if (data.warning) {
            problem = data.warning
        } else {
            problem = ""
        }

        if (data.state === "running") {
            for (var i = 0; i < data.hosts.length; i++) {
                var loopback = data.hosts[i].loopback
                if (loopback && !added[loopback]) {
                    added[loopback] = true
                    ComputerManager.addNewHostManually(loopback)
                }
            }
        }

        if (!firstRunHandled) {
            firstRunHandled = true
            if (nodelightFirstRun === "1" && data.hosts.length === 0) {
                openTailscalePage()
            }
        }
    }

    function refresh() {
        if (busy || !available) {
            return
        }
        call("GET", "/api/status", null, function(code, data) {
            if (code === 200 && data) {
                apply(data)
            } else {
                problem = qsTr("Nodelight is not responding")
            }
        })
    }

    // POST/PUT then show the returned status (or the error).
    function send(method, path, body) {
        call(method, path, body, function(code, data) {
            if (code >= 200 && code < 300 && data) {
                apply(data)
            } else if (data && data.error) {
                problem = data.error
            } else {
                problem = qsTr("The request failed")
            }
        })
    }

    Timer {
        interval: 2000
        repeat: true
        running: agent.available
        triggeredOnStart: true
        onTriggered: agent.refresh()
    }
}
