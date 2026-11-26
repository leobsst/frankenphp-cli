import Capacitor
import Foundation

/// Display of the app (Settings › Text size, src/lib/display.ts). `info` tells the iPad app running on a Mac, which
/// macOS draws at 77 %: its automatic text size makes up for it. The size itself is a CSS zoom of the page (main.css):
/// the native zoom of the WebView (`pageZoom`) kept the layout at the unzoomed width, so the page overflowed the
/// window.
@objc(DisplayPlugin)
public class DisplayPlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "DisplayPlugin"
    public let jsName = "Display"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "info", returnType: CAPPluginReturnPromise),
    ]

    /// Resolves `{ onMac }`.
    @objc func info(_ call: CAPPluginCall) {
        call.resolve(["onMac": ProcessInfo.processInfo.isiOSAppOnMac])
    }
}
