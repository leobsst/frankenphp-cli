import Capacitor
import Foundation
import LocalAuthentication

/// Lock of the app (src/lib/appLock.ts): Face ID or Touch ID, with the device passcode as fallback.
///
/// The policy is `deviceOwnerAuthentication`: after failed biometric attempts, or without biometry, iOS offers the
/// passcode, so a face that is not recognised never locks the owner out of the app.
@objc(AppLockPlugin)
public class AppLockPlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "AppLockPlugin"
    public let jsName = "AppLock"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "status", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "authenticate", returnType: CAPPluginReturnPromise),
    ]

    /// Resolves `{ available, biometry }`: whether the device can authenticate its owner (biometry or passcode), and
    /// the biometry it has (`faceId`, `touchId` or `none`).
    @objc func status(_ call: CAPPluginCall) {
        let context = LAContext()
        let available = context.canEvaluatePolicy(.deviceOwnerAuthentication, error: nil)
        // biometryType is only set once a biometric policy has been evaluated.
        _ = context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil)
        let biometry: String
        switch context.biometryType {
        case .faceID: biometry = "faceId"
        case .touchID: biometry = "touchId"
        default: biometry = "none"
        }
        call.resolve(["available": available, "biometry": biometry])
    }

    /// Asks the owner to authenticate; resolves on success, rejects with the code `cancelled`, `unavailable` or
    /// `failed` otherwise.
    @objc func authenticate(_ call: CAPPluginCall) {
        let context = LAContext()
        if let cancel = call.getString("cancelTitle") {
            context.localizedCancelTitle = cancel
        }
        let reason = call.getString("reason") ?? "FrankenManager"
        context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason) { success, error in
            if success {
                call.resolve()
                return
            }
            let code: String
            switch (error as? LAError)?.code {
            case .userCancel, .appCancel, .systemCancel, .userFallback: code = "cancelled"
            case .passcodeNotSet, .biometryNotAvailable, .biometryNotEnrolled: code = "unavailable"
            default: code = "failed"
            }
            call.reject(error?.localizedDescription ?? "Authentication failed.", code)
        }
    }
}
