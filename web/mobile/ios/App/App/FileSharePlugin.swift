import Capacitor
import UIKit

/// Share sheet of a text file (src/lib/share.ts): the WebView ignores the downloads of the page (the logs export), so
/// the file is written to the temporary directory and handed to the system share sheet (Files, AirDrop, Mail…).
@objc(FileSharePlugin)
public class FileSharePlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "FileSharePlugin"
    public let jsName = "FileShare"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "shareText", returnType: CAPPluginReturnPromise),
    ]

    /// Writes `text` as the file `name` and shows the share sheet. Resolves `{ completed }`: false when cancelled.
    @objc func shareText(_ call: CAPPluginCall) {
        // The file name only: no directory taken from the page.
        let name = URL(fileURLWithPath: call.getString("name") ?? "file.txt").lastPathComponent
        let text = call.getString("text") ?? ""
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("shared", isDirectory: true)
        let file = dir.appendingPathComponent(name)
        do {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try text.write(to: file, atomically: true, encoding: .utf8)
        } catch {
            call.reject(error.localizedDescription)
            return
        }
        DispatchQueue.main.async {
            guard var presenter = self.bridge?.viewController else {
                call.reject("App view not found.")
                return
            }
            while let presented = presenter.presentedViewController { presenter = presented }
            let sheet = UIActivityViewController(activityItems: [file], applicationActivities: nil)
            sheet.completionWithItemsHandler = { _, completed, _, error in
                if let error { call.reject(error.localizedDescription) } else { call.resolve(["completed": completed]) }
            }
            // iPad (and the iPad app on a Mac): a popover, which needs an anchor; centred, without an arrow.
            if let popover = sheet.popoverPresentationController {
                popover.sourceView = presenter.view
                popover.sourceRect = CGRect(x: presenter.view.bounds.midX, y: presenter.view.bounds.midY, width: 0, height: 0)
                popover.permittedArrowDirections = []
            }
            presenter.present(sheet, animated: true)
        }
    }
}
