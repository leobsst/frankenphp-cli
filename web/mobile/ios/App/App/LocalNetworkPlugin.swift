import Capacitor
import Foundation

/// Local network of the device (src/lib/localNetwork.ts): its private IPv4 address on the Wi-Fi (or the Ethernet of
/// an iPad or a Mac) and the prefix length of that network, for the address placeholder of the instance form. Read
/// from the interfaces themselves (getifaddrs): no permission, nothing sent anywhere.
@objc(LocalNetworkPlugin)
public class LocalNetworkPlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "LocalNetworkPlugin"
    public let jsName = "LocalNetwork"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "ipv4", returnType: CAPPluginReturnPromise),
    ]

    /// Resolves `{ ip, prefix }`, or `{}` without a private IPv4 address on an `en` interface (cellular data only).
    @objc func ipv4(_ call: CAPPluginCall) {
        var head: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&head) == 0, let first = head else {
            call.resolve([:])
            return
        }
        defer { freeifaddrs(head) }

        var found: [(name: String, ip: String, prefix: Int)] = []
        for pointer in sequence(first: first, next: { $0.pointee.ifa_next }) {
            let ifa = pointer.pointee
            let flags = ifa.ifa_flags
            guard flags & UInt32(IFF_UP) != 0, flags & UInt32(IFF_RUNNING) != 0, flags & UInt32(IFF_LOOPBACK) == 0,
                  let addr = ifa.ifa_addr, addr.pointee.sa_family == sa_family_t(AF_INET),
                  let mask = ifa.ifa_netmask else { continue }
            let name = String(cString: ifa.ifa_name)
            // en0: the Wi-Fi of an iPhone or an iPad; the others, Ethernet adapters.
            guard name.hasPrefix("en") else { continue }
            let ip = addr.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { UInt32(bigEndian: $0.pointee.sin_addr.s_addr) }
            let bits = mask.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { $0.pointee.sin_addr.s_addr }
            let a = ip >> 24, b = (ip >> 16) & 0xff
            // Private networks only (10/8, 172.16/12, 192.168/16).
            guard a == 10 || (a == 172 && (16...31).contains(b)) || (a == 192 && b == 168) else { continue }
            found.append((name, "\(a).\(b).\((ip >> 8) & 0xff).\(ip & 0xff)", bits.nonzeroBitCount))
        }
        guard let best = found.min(by: { $0.name.count != $1.name.count ? $0.name.count < $1.name.count : $0.name < $1.name }) else {
            call.resolve([:])
            return
        }
        call.resolve(["ip": best.ip, "prefix": best.prefix])
    }
}
