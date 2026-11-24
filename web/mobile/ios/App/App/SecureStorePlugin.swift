import Capacitor
import Foundation
import Security

/// Secret storage in the iOS Keychain, for the session tokens of the instances (src/lib/secureStore.ts).
///
/// Each value is a generic password item of the app's own service, readable once the device has been unlocked after a
/// restart, and never synced to iCloud nor restored on another device (`ThisDeviceOnly`): a session token stays on
/// the phone that opened the session.
@objc(SecureStorePlugin)
public class SecureStorePlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "SecureStorePlugin"
    public let jsName = "SecureStore"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "get", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "set", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "remove", returnType: CAPPluginReturnPromise),
    ]

    private var service: String {
        (Bundle.main.bundleIdentifier ?? "frankenmanager") + ".secure-store"
    }

    private func query(_ key: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
    }

    /// Resolves `{ value }`, null when the key is not stored.
    @objc func get(_ call: CAPPluginCall) {
        guard let key = call.getString("key"), !key.isEmpty else {
            call.reject("Missing key.")
            return
        }
        var q = query(key)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var item: CFTypeRef?
        let status = SecItemCopyMatching(q as CFDictionary, &item)
        switch status {
        case errSecSuccess:
            guard let data = item as? Data, let value = String(data: data, encoding: .utf8) else {
                call.reject("Unreadable Keychain item.")
                return
            }
            call.resolve(["value": value])
        case errSecItemNotFound:
            call.resolve(["value": NSNull()])
        default:
            call.reject("Keychain read failed (\(status)).")
        }
    }

    /// Stores `value` under `key`, replacing a previous value.
    @objc func set(_ call: CAPPluginCall) {
        guard let key = call.getString("key"), !key.isEmpty, let value = call.getString("value") else {
            call.reject("Missing key or value.")
            return
        }
        let data = Data(value.utf8)
        let attributes: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        var status = SecItemUpdate(query(key) as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            let add = query(key).merging(attributes) { _, new in new }
            status = SecItemAdd(add as CFDictionary, nil)
        }
        if status == errSecSuccess {
            call.resolve()
        } else {
            call.reject("Keychain write failed (\(status)).")
        }
    }

    /// Deletes `key`; no error when it is not stored.
    @objc func remove(_ call: CAPPluginCall) {
        guard let key = call.getString("key"), !key.isEmpty else {
            call.reject("Missing key.")
            return
        }
        let status = SecItemDelete(query(key) as CFDictionary)
        if status == errSecSuccess || status == errSecItemNotFound {
            call.resolve()
        } else {
            call.reject("Keychain delete failed (\(status)).")
        }
    }
}
