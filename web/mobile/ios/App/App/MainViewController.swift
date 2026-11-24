import Capacitor
import UIKit

/// Capacitor controller of the app: registers the plugins written in the app itself (outside npm packages).
class MainViewController: CAPBridgeViewController {
    override func capacitorDidLoad() {
        bridge?.registerPluginInstance(NativeTabBarPlugin())
    }
}
