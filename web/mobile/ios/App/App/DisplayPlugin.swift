import Capacitor
import Foundation
import WebKit

/// Interface scale of the app (Settings › Text size, src/lib/display.ts).
///
/// The WebView zooms natively (`pageZoom`, like the zoom of Safari): the layout is computed again at that scale, so
/// the breakpoints and the full-screen heights stay right, which a CSS zoom does not give. `info` tells the iPad app
/// running on a Mac, which macOS draws at 77 %: its automatic size makes up for it.
@objc(DisplayPlugin)
public class DisplayPlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "DisplayPlugin"
    public let jsName = "Display"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "info", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "setZoom", returnType: CAPPluginReturnPromise),
    ]

    /// Resolves `{ onMac }`.
    @objc func info(_ call: CAPPluginCall) {
        call.resolve(["onMac": ProcessInfo.processInfo.isiOSAppOnMac])
    }

    /// Sets the zoom of the WebView to `scale` (0.5 to 3).
    @objc func setZoom(_ call: CAPPluginCall) {
        let scale = min(max(call.getDouble("scale") ?? 1, 0.5), 3)
        DispatchQueue.main.async {
            self.bridge?.webView?.pageZoom = CGFloat(scale)
            call.resolve()
        }
    }
}
