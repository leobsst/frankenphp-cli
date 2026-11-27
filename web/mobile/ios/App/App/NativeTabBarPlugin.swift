import Capacitor
import UIKit

/// Native tab bar (UITabBar) laid over the web view, at the bottom: built with the iOS 26 SDK or later, it takes the
/// Liquid Glass look; before iOS 26, the classic blurred look. The dashboard (Vue) gives it its tabs and follows the
/// taps through the `tabSelected` event. On Android and in the browser, the bar stays in HTML.
@objc(NativeTabBarPlugin)
public class NativeTabBarPlugin: CAPPlugin, CAPBridgedPlugin, UITabBarDelegate {
    public let identifier = "NativeTabBarPlugin"
    public let jsName = "NativeTabBar"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "show", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "select", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "setHidden", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "remove", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "setAppearance", returnType: CAPPluginReturnPromise),
    ]

    private var tabBar: UITabBar?
    /// Tab ids, in order (the tag of a UITabBarItem is its index).
    private var ids: [String] = []
    /// Colours of the active and of the other tabs, and look (light, dark or system) of the page, kept for a
    /// recreated bar.
    private var tint: UIColor?
    private var normal: UIColor?
    private var theme = "system"

    /// Shows the bar with `items` ([{ id, title, icon }], icon: name of an SF Symbol) and the `selected` tab.
    /// Resolves `height`: the height it covers at the bottom of the web view, home indicator area included.
    @objc func show(_ call: CAPPluginCall) {
        let items = call.getArray("items", JSObject.self) ?? []
        let selected = call.getString("selected")
        DispatchQueue.main.async {
            guard let view = self.bridge?.viewController?.view else {
                call.reject("App view not found.")
                return
            }
            let bar = self.tabBar ?? self.makeTabBar(in: view)
            self.applyAppearance(bar)
            self.ids = items.compactMap { $0["id"] as? String }
            bar.items = items.enumerated().map { index, item in
                let icon = (item["icon"] as? String).flatMap { UIImage(systemName: $0) } ?? UIImage(systemName: "circle")
                return UITabBarItem(title: item["title"] as? String, image: icon, tag: index)
            }
            self.select(bar, id: selected)
            bar.isHidden = false
            bar.alpha = 1
            view.layoutIfNeeded()
            call.resolve(["height": view.bounds.height - bar.frame.minY])
        }
    }

    /// Highlights the `id` tab, or none (page outside the tabs) without `id`.
    @objc func select(_ call: CAPPluginCall) {
        let id = call.getString("id")
        DispatchQueue.main.async {
            if let bar = self.tabBar { self.select(bar, id: id) }
            call.resolve()
        }
    }

    /// Hides the bar (modal, side menu open) or shows it again, with a fade.
    @objc func setHidden(_ call: CAPPluginCall) {
        let hidden = call.getBool("hidden") ?? true
        DispatchQueue.main.async {
            guard let bar = self.tabBar else { call.resolve(); return }
            if !hidden { bar.isHidden = false }
            UIView.animate(withDuration: 0.2, animations: { bar.alpha = hidden ? 0 : 1 }) { _ in
                if hidden { bar.isHidden = true }
            }
            call.resolve()
        }
    }

    /// Removes the bar (back to the instance list).
    @objc func remove(_ call: CAPPluginCall) {
        DispatchQueue.main.async {
            self.tabBar?.removeFromSuperview()
            self.tabBar = nil
            call.resolve()
        }
    }

    /// Colours of the BottomNav it replaces, as the page shows them (#RRGGBB, read by src/lib/nativeTabBar.ts after
    /// each change): `tint` for the active tab (--ui-primary), `normal` for the others (--ui-text-muted), and the
    /// `theme` of the page (light or dark; system to follow iOS). Called again on every change of the page's theme or
    /// colours, the system appearance included.
    @objc func setAppearance(_ call: CAPPluginCall) {
        let tint = call.getString("tint").flatMap(Self.color(hex:))
        let normal = call.getString("normal").flatMap(Self.color(hex:))
        let theme = call.getString("theme") ?? "system"
        DispatchQueue.main.async {
            self.tint = tint
            self.normal = normal
            self.theme = theme
            if let bar = self.tabBar { self.applyAppearance(bar) }
            call.resolve()
        }
    }

    private func applyAppearance(_ bar: UITabBar) {
        bar.overrideUserInterfaceStyle = theme == "dark" ? .dark : theme == "light" ? .light : .unspecified
        bar.tintColor = tint
        // The classic bar (before iOS 26) takes it; the Liquid Glass bar keeps the other tabs in its own vibrant colour,
        // legible over the glass, whatever is asked (UITabBarAppearance included).
        bar.unselectedItemTintColor = normal
    }

    private static func color(hex: String) -> UIColor? {
        let digits = hex.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: "#", with: "")
        guard digits.count == 6, let value = UInt32(digits, radix: 16) else { return nil }
        return UIColor(red: CGFloat((value >> 16) & 0xFF) / 255, green: CGFloat((value >> 8) & 0xFF) / 255,
                       blue: CGFloat(value & 0xFF) / 255, alpha: 1)
    }

    public func tabBar(_ tabBar: UITabBar, didSelect item: UITabBarItem) {
        guard ids.indices.contains(item.tag) else { return }
        notifyListeners("tabSelected", data: ["id": ids[item.tag]])
    }

    private func select(_ bar: UITabBar, id: String?) {
        bar.selectedItem = id.flatMap { ids.firstIndex(of: $0) }.flatMap { bar.items?[$0] }
    }

    private func makeTabBar(in view: UIView) -> UITabBar {
        let bar = UITabBar()
        bar.delegate = self
        bar.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(bar)
        // Pinned to the bottom of the view: UIKit adds the height of the home indicator area to it.
        NSLayoutConstraint.activate([
            bar.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            bar.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            bar.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
        tabBar = bar
        return bar
    }
}
