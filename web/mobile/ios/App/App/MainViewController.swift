import Capacitor
import UIKit
import WebKit

/// Capacitor controller of the app: registers the plugins written in the app itself (outside npm packages).
class MainViewController: CAPBridgeViewController {
    override func capacitorDidLoad() {
        bridge?.registerPluginInstance(NativeTabBarPlugin())
        bridge?.registerPluginInstance(NativeSegmentsPlugin())
        bridge?.registerPluginInstance(SecureStorePlugin())
        bridge?.registerPluginInstance(AppLockPlugin())
        bridge?.registerPluginInstance(DisplayPlugin())
        bridge?.registerPluginInstance(LocalNetworkPlugin())
    }

    /// Pinch zoom stays possible (accessibility), like in Safari: the maximum-scale of the viewport (index.html) only
    /// stops the automatic zoom on a focused field.
    override func webViewConfiguration(for instanceConfiguration: InstanceConfiguration) -> WKWebViewConfiguration {
        let configuration = super.webViewConfiguration(for: instanceConfiguration)
        configuration.ignoresViewportScaleLimits = true
        return configuration
    }
}
