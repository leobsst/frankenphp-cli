import Capacitor
import UIKit

/// Native tabs of the dashboard pages (UISegmentedControl) in a bar pinned below the HTML header, over the web view:
/// the content scrolls under it, like under a navigation bar. The pages give it their tabs (src/lib/nativeSegments.ts)
/// and follow the taps through the `segmentSelected` event. On Android and in the browser, the tabs stay in HTML.
@objc(NativeSegmentsPlugin)
public class NativeSegmentsPlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "NativeSegmentsPlugin"
    public let jsName = "NativeSegments"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "show", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "hide", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "setHidden", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "setAppearance", returnType: CAPPluginReturnPromise),
    ]

    /// Height of the bar: the control (32) and its margins.
    private static let height: CGFloat = 48

    private var bar: UIView?
    private var control: UISegmentedControl?
    private var scroller: UIScrollView?
    /// Space between the top safe area and the bar: the HTML header, zoomed.
    private var topConstraint: NSLayoutConstraint?
    /// Tab ids, in segment order.
    private var ids: [String] = []
    private var titles: [String] = []
    /// Theme of the dashboard (light, dark or system), kept for a recreated bar.
    private var theme = "system"

    /// Shows the bar with `items` ([{ id, title }]) and the `selected` tab, `top` points below the top safe area
    /// (already `hidden` if a modal is open). Resolves `height`: the height it covers below the header.
    @objc func show(_ call: CAPPluginCall) {
        let items = call.getArray("items", JSObject.self) ?? []
        let selected = call.getString("selected")
        let top = CGFloat(call.getDouble("top") ?? 0)
        let hidden = call.getBool("hidden") ?? false
        DispatchQueue.main.async {
            guard let view = self.bridge?.viewController?.view else {
                call.reject("App view not found.")
                return
            }
            if self.bar == nil { self.makeBar(in: view) }
            guard let bar = self.bar, let control = self.control else { return }
            self.topConstraint?.constant = top
            self.applyAppearance()

            let ids = items.compactMap { $0["id"] as? String }
            let titles = items.map { $0["title"] as? String ?? "" }
            if ids != self.ids || titles != self.titles {
                self.ids = ids
                self.titles = titles
                control.removeAllSegments()
                for (index, title) in titles.enumerated() {
                    control.insertSegment(withTitle: title, at: index, animated: false)
                }
            }
            control.selectedSegmentIndex = selected.flatMap { ids.firstIndex(of: $0) } ?? UISegmentedControl.noSegment
            bar.isHidden = hidden
            bar.alpha = hidden ? 0 : 1
            view.layoutIfNeeded()
            self.revealSelected()
            call.resolve(["height": Self.height])
        }
    }

    /// Removes the bar (page without tabs, back to the instance list).
    @objc func hide(_ call: CAPPluginCall) {
        DispatchQueue.main.async {
            self.bar?.removeFromSuperview()
            self.bar = nil
            self.control = nil
            self.scroller = nil
            self.topConstraint = nil
            self.ids = []
            self.titles = []
            call.resolve()
        }
    }

    /// Hides the bar (modal, side menu, toast) or shows it again, with a fade.
    @objc func setHidden(_ call: CAPPluginCall) {
        let hidden = call.getBool("hidden") ?? true
        DispatchQueue.main.async {
            guard let bar = self.bar else { call.resolve(); return }
            if !hidden { bar.isHidden = false }
            UIView.animate(withDuration: 0.2, animations: { bar.alpha = hidden ? 0 : 1 }) { _ in
                if hidden && bar.alpha == 0 { bar.isHidden = true }
            }
            call.resolve()
        }
    }

    /// Theme of the bar (`theme`: light, dark, or system to follow iOS), as the dashboard's.
    @objc func setAppearance(_ call: CAPPluginCall) {
        let theme = call.getString("theme") ?? "system"
        DispatchQueue.main.async {
            self.theme = theme
            self.applyAppearance()
            call.resolve()
        }
    }

    private func applyAppearance() {
        bar?.overrideUserInterfaceStyle = theme == "dark" ? .dark : theme == "light" ? .light : .unspecified
    }

    @objc private func changed(_ control: UISegmentedControl) {
        revealSelected()
        guard ids.indices.contains(control.selectedSegmentIndex) else { return }
        notifyListeners("segmentSelected", data: ["id": ids[control.selectedSegmentIndex]])
    }

    /// Long labels: the control scrolls sideways, the selected tab is brought into view.
    private func revealSelected() {
        guard let control, let scroller, control.selectedSegmentIndex != UISegmentedControl.noSegment,
              control.numberOfSegments > 0, scroller.contentSize.width > scroller.bounds.width else { return }
        // Segment widths follow their labels: the position in proportion is close enough to bring it into view.
        let width = control.bounds.width / CGFloat(control.numberOfSegments)
        let frame = CGRect(x: width * CGFloat(control.selectedSegmentIndex), y: 0, width: width, height: control.bounds.height)
        scroller.scrollRectToVisible(control.convert(frame, to: scroller).insetBy(dx: -16, dy: 0), animated: true)
    }

    private func makeBar(in view: UIView) {
        // Material background (the content scrolls under it) and a hairline at the bottom, like a navigation bar.
        let bar = UIVisualEffectView(effect: UIBlurEffect(style: .systemChromeMaterial))
        bar.translatesAutoresizingMaskIntoConstraints = false
        let hairline = UIView()
        hairline.backgroundColor = .separator
        hairline.translatesAutoresizingMaskIntoConstraints = false

        let scroller = UIScrollView()
        scroller.showsHorizontalScrollIndicator = false
        scroller.alwaysBounceHorizontal = false
        scroller.translatesAutoresizingMaskIntoConstraints = false

        let control = UISegmentedControl()
        control.apportionsSegmentWidthsByContent = true
        control.translatesAutoresizingMaskIntoConstraints = false
        control.addTarget(self, action: #selector(changed(_:)), for: .valueChanged)

        view.addSubview(bar)
        bar.contentView.addSubview(scroller)
        bar.contentView.addSubview(hairline)
        scroller.addSubview(control)

        let top = bar.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor)
        let margins = bar.contentView.safeAreaLayoutGuide
        NSLayoutConstraint.activate([
            top,
            bar.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            bar.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            bar.heightAnchor.constraint(equalToConstant: Self.height),

            scroller.leadingAnchor.constraint(equalTo: margins.leadingAnchor),
            scroller.trailingAnchor.constraint(equalTo: margins.trailingAnchor),
            scroller.centerYAnchor.constraint(equalTo: bar.contentView.centerYAnchor),
            scroller.heightAnchor.constraint(equalToConstant: 32),

            // The control fills the width when its labels fit, and scrolls past it otherwise (16 pt margins, like the
            // gutters of the page).
            control.leadingAnchor.constraint(equalTo: scroller.contentLayoutGuide.leadingAnchor, constant: 16),
            control.trailingAnchor.constraint(equalTo: scroller.contentLayoutGuide.trailingAnchor, constant: -16),
            control.topAnchor.constraint(equalTo: scroller.contentLayoutGuide.topAnchor),
            control.bottomAnchor.constraint(equalTo: scroller.contentLayoutGuide.bottomAnchor),
            control.heightAnchor.constraint(equalTo: scroller.frameLayoutGuide.heightAnchor),
            control.widthAnchor.constraint(greaterThanOrEqualTo: scroller.frameLayoutGuide.widthAnchor, constant: -32),

            hairline.leadingAnchor.constraint(equalTo: bar.leadingAnchor),
            hairline.trailingAnchor.constraint(equalTo: bar.trailingAnchor),
            hairline.bottomAnchor.constraint(equalTo: bar.bottomAnchor),
            hairline.heightAnchor.constraint(equalToConstant: 1 / max(view.traitCollection.displayScale, 1)),
        ])

        self.bar = bar
        self.control = control
        self.scroller = scroller
        topConstraint = top
    }
}
