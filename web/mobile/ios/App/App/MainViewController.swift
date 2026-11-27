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
        bridge?.registerPluginInstance(FileSharePlugin())

        // iOS 26: the scroll view of the web view shades the status bar (scroll edge effect), for content scrolling
        // beneath it. The web view itself never scrolls (capacitor.config.ts, scrollEnabled: false): each screen
        // scrolls in its own container, below an opaque header, so the shade only darkened the top of the screen.
        if #available(iOS 26.0, *), let scrollView = webView?.scrollView {
            scrollView.topEdgeEffect.isHidden = true
        }
    }

    /// Pinch zoom stays possible (accessibility), like in Safari: the maximum-scale of the viewport (index.html) only
    /// stops the automatic zoom on a focused field.
    override func webViewConfiguration(for instanceConfiguration: InstanceConfiguration) -> WKWebViewConfiguration {
        let configuration = super.webViewConfiguration(for: instanceConfiguration)
        configuration.ignoresViewportScaleLimits = true
        return configuration
    }
}
