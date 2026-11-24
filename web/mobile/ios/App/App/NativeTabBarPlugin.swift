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
    /// Primary colour of the app (#RRGGBB) and its theme (light, dark or system), kept for a recreated bar.
    private var tint: UIColor?
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

    /// Tint of the active tab (the primary colour chosen in the app, `tint` as #RRGGBB) and theme of the bar
    /// (`theme`: light, dark, or system to follow iOS), as the dashboard's.
    @objc func setAppearance(_ call: CAPPluginCall) {
        let hex = call.getString("tint")
        let theme = call.getString("theme") ?? "system"
        DispatchQueue.main.async {
            self.tint = hex.flatMap(Self.color(hex:))
            self.theme = theme
            if let bar = self.tabBar { self.applyAppearance(bar) }
            call.resolve()
        }
    }

    private func applyAppearance(_ bar: UITabBar) {
        bar.overrideUserInterfaceStyle = theme == "dark" ? .dark : theme == "light" ? .light : .unspecified
        guard let base = tint else {
            bar.tintColor = nil
            return
        }
        // In dark mode, the dashboard lightens its primary colour (brand-400): same here, for the contrast.
        bar.tintColor = UIColor { traits in
            traits.userInterfaceStyle == .dark ? Self.mix(base, with: .white, amount: 0.4) : base
        }
    }

    private static func color(hex: String) -> UIColor? {
        let digits = hex.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: "#", with: "")
        guard digits.count == 6, let value = UInt32(digits, radix: 16) else { return nil }
        return UIColor(red: CGFloat((value >> 16) & 0xFF) / 255, green: CGFloat((value >> 8) & 0xFF) / 255,
                       blue: CGFloat(value & 0xFF) / 255, alpha: 1)
    }

    private static func mix(_ a: UIColor, with b: UIColor, amount: CGFloat) -> UIColor {
        var (r1, g1, b1, a1, r2, g2, b2, a2): (CGFloat, CGFloat, CGFloat, CGFloat, CGFloat, CGFloat, CGFloat, CGFloat) = (0, 0, 0, 0, 0, 0, 0, 0)
        a.getRed(&r1, green: &g1, blue: &b1, alpha: &a1)
        b.getRed(&r2, green: &g2, blue: &b2, alpha: &a2)
        return UIColor(red: r1 + (r2 - r1) * amount, green: g1 + (g2 - g1) * amount, blue: b1 + (b2 - b1) * amount, alpha: a1)
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
