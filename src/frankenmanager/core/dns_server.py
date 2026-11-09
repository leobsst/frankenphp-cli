"""Optional LAN DNS server resolving the registered domains to this host.

Off by default: set DNS_SERVER=true in .env to turn it on. Other devices on
the network then use this machine as their DNS server (set by hand, or handed
out by the router via DHCP) and reach every registered domain without editing
their own hosts file. Every other query is forwarded to DNS_UPSTREAM.

The server is a CoreDNS container published on every interface. It reloads its
records file on its own, so domain changes made while the server runs only need
`sync`, not a restart. LAN IP changes are followed the same way (see dns_watcher).
"""

import ipaddress
import os
import re
import shutil
import socket
import subprocess
from pathlib import Path
from typing import TYPE_CHECKING, Optional

from ..exceptions import ConfigurationError
from . import soketi
from .ca_server import get_lan_ip

if TYPE_CHECKING:
    from .database import DatabaseManager
    from .environment import EnvironmentManager

CONTAINER_NAME = "franken_dns"
IMAGE = "coredns/coredns:1.11.3"
DEFAULT_PORT = "53"
# Every interface, so a network change (Wi-Fi <-> Ethernet) needs no reconfiguration
DEFAULT_LISTEN_IP = "0.0.0.0"
# Public resolvers rather than the host's own: once the router hands this
# server out via DHCP, the host itself may resolve through it, and forwarding
# to the host's resolver would then loop back here.
DEFAULT_UPSTREAM = "1.1.1.1 9.9.9.9"
# Kept short so that removed domains stop resolving quickly on client devices
RECORDS_TTL = 30
# Interfaces of containers and VMs (Docker, libvirt, VirtualBox, VMware...):
# their IPs are not reachable from the other devices on the LAN
VIRTUAL_INTERFACE_PREFIXES = (
    "docker",
    "br-",
    "veth",
    "virbr",
    "vboxnet",
    "vmnet",
    "cni",
    "flannel",
    "podman",
)


def is_enabled(env: "EnvironmentManager") -> bool:
    """Check if the LAN DNS server is enabled.

    Returns:
        True if DNS_SERVER is 'true'.
    """
    return env.get("DNS_SERVER") == "true"


def get_dns_dir(project_dir: Path) -> Path:
    """Directory holding the CoreDNS config, mounted into the container."""
    return project_dir / "dns"


def resolve_ip(env: "EnvironmentManager") -> str:
    """IP every registered domain resolves to.

    Returns:
        DNS_IP from .env, or the host's current LAN IP when unset.

    Raises:
        ConfigurationError: If DNS_IP is not a valid IP address.
    """
    configured = env.get("DNS_IP")
    if not configured:
        return get_lan_ip()
    return _parse_ip("DNS_IP", configured)


def _parse_ip(name: str, value: str) -> str:
    try:
        return str(ipaddress.ip_address(value))
    except ValueError as e:
        raise ConfigurationError(f"Invalid {name} '{value}': {e}") from e


def get_listen_address(env: "EnvironmentManager") -> Optional[str]:
    """Host address the DNS server is published on.

    Defaults to every interface (0.0.0.0), so the server keeps answering when
    the host switches network (Wi-Fi to Ethernet, new DHCP lease...).
    DNS_LISTEN_IP pins it to one address instead, e.g. to avoid clashing with
    a local resolver already bound to port 53 (systemd-resolved on Linux).

    Returns:
        "ip:port", or None when the DNS server is disabled.

    Raises:
        ConfigurationError: If DNS_LISTEN_IP is not a valid IP address.
    """
    if not is_enabled(env):
        return None
    configured = env.get("DNS_LISTEN_IP")
    ip = _parse_ip("DNS_LISTEN_IP", configured) if configured else DEFAULT_LISTEN_IP
    host = f"[{ip}]" if ":" in ip else ip
    return f"{host}:{env.get('DNS_PORT') or DEFAULT_PORT}"


def parse_ip_addr(output: str) -> list[tuple[str, str]]:
    """Parse `ip -o -4 addr show` (Linux).

    Returns:
        (interface, IPv4) pairs, in output order.
    """
    return re.findall(r"^\d+:\s+(\S+)\s+inet\s+([\d.]+)/", output, re.M)


def parse_ifconfig(output: str) -> list[tuple[str, str]]:
    """Parse `ifconfig` (macOS, BSD).

    Returns:
        (interface, IPv4) pairs, in output order.
    """
    pairs: list[tuple[str, str]] = []
    interface = ""
    for line in output.splitlines():
        header = re.match(r"^(\S+?):?\s", line)
        if header:
            interface = header.group(1)
            continue
        inet = re.match(r"^\s+inet\s+(?:addr:)?([\d.]+)", line)
        if inet and interface:
            pairs.append((interface, inet.group(1)))
    return pairs


def _run(cmd: list[str]) -> Optional[str]:
    """Run a command, returning its output (None if missing or failed)."""
    if not shutil.which(cmd[0]):
        return None
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=5)
    except (OSError, subprocess.SubprocessError):
        return None
    return result.stdout if result.returncode == 0 else None


def _is_lan_candidate(interface: str, ip: str) -> bool:
    """Whether another device could reach this host through this address."""
    if interface.startswith(VIRTUAL_INTERFACE_PREFIXES):
        return False
    try:
        address = ipaddress.ip_address(ip)
    except ValueError:
        return False
    return not (address.is_loopback or address.is_link_local or address.is_unspecified)


def list_interface_ips() -> list[tuple[str, str]]:
    """List this host's IPv4 addresses reachable from the LAN.

    Reads `ip` (Linux) or `ifconfig` (macOS, BSD), falling back to the host
    name resolution (Windows), where the interface name is unknown ("").
    Loopback, link-local and container/VM interfaces are left out.

    Returns:
        De-duplicated (interface, IPv4) pairs.
    """
    output = _run(["ip", "-o", "-4", "addr", "show"])
    if output is not None:
        pairs = parse_ip_addr(output)
    else:
        output = _run(["ifconfig"])
        if output is not None:
            pairs = parse_ifconfig(output)
        else:
            try:
                infos = socket.getaddrinfo(socket.gethostname(), None, socket.AF_INET)
            except OSError:
                infos = []
            pairs = [("", str(info[4][0])) for info in infos]

    seen: set[str] = set()
    result: list[tuple[str, str]] = []
    for interface, ip in pairs:
        if ip not in seen and _is_lan_candidate(interface, ip):
            seen.add(ip)
            result.append((interface, ip))
    return result


def get_server_addresses(env: "EnvironmentManager") -> list[tuple[str, str]]:
    """Addresses the other devices can use as their DNS server.

    Every LAN address of this host when listening on every interface (the
    default), only DNS_LISTEN_IP otherwise. The address the domains resolve
    to comes first, as it is on the network the host currently routes through.

    Returns:
        (interface, IPv4) pairs, interface "" when unknown.
    """
    configured = env.get("DNS_LISTEN_IP")
    if configured and _parse_ip("DNS_LISTEN_IP", configured) != DEFAULT_LISTEN_IP:
        return [("", _parse_ip("DNS_LISTEN_IP", configured))]

    addresses = list_interface_ips()
    records_ip = resolve_ip(env)
    addresses.sort(key=lambda pair: pair[1] != records_ip)
    if not addresses and not records_ip.startswith("127."):
        addresses = [("", records_ip)]
    return addresses


def describe_server_addresses(addresses: list[tuple[str, str]], records_ip: str) -> list[str]:
    """Lines telling which addresses to give the other devices as DNS server.

    Shared by `start` and `status`, so both show the same list.

    Args:
        addresses: (interface, IP) pairs, from get_server_addresses().
        records_ip: IP the domains resolve to.

    Returns:
        Lines to print (indented), empty when no address is known.
    """
    if not addresses:
        return []
    if len(addresses) == 1:
        return [
            f"  Set {addresses[0][1]} as the DNS server of your other devices (or of your router)."
        ]

    lines = ["  Set one of these IPs as the DNS server of your other devices (or of your router):"]
    for interface, ip in addresses:
        details = [interface] if interface else []
        if ip == records_ip:
            details.append("domains resolve to it")
        suffix = f" ({', '.join(details)})" if details else ""
        lines.append(f"    - {ip}{suffix}")
    lines.append(
        "  On the same network, set two of them as primary and secondary DNS servers: "
        "the devices keep resolving if one interface goes down."
    )
    return lines


def build_corefile(upstream: str) -> str:
    """Build the CoreDNS configuration.

    Args:
        upstream: Space-separated resolvers to forward non-local queries to.

    Returns:
        Corefile content.
    """
    return (
        ".:53 {\n"
        "    hosts /etc/coredns/hosts {\n"
        f"        ttl {RECORDS_TTL}\n"
        "        reload 2s\n"
        "        fallthrough\n"
        "    }\n"
        f"    forward . {upstream}\n"
        f"    cache {RECORDS_TTL}\n"
        "    errors\n"
        "}\n"
    )


def build_records(ip: str, domains: list[str]) -> str:
    """Build the hosts-format records file pointing every domain at `ip`.

    Args:
        ip: Address the domains resolve to.
        domains: Domains to resolve.

    Returns:
        Records file content.
    """
    lines = ["# Generated by FrankenManager, rewritten on every domain change."]
    lines.extend(f"{ip} {domain}" for domain in sorted(set(domains)))
    return "\n".join(lines) + "\n"


def _write_atomic(path: Path, content: str) -> None:
    """Replace `path` in one step, so CoreDNS never reloads a half-written file."""
    tmp_path = path.with_name(f".{path.name}.tmp")
    tmp_path.write_text(content)
    os.replace(tmp_path, path)


def sync(project_dir: Path, env: "EnvironmentManager", db: "DatabaseManager") -> None:
    """Write the CoreDNS config and records for every registered domain.

    Covers regular domains, alternate hosts and proxy hosts alike, since all
    of them are served by this host's reverse proxy. Does nothing when the
    DNS server is disabled.

    Args:
        project_dir: Path to the project directory.
        env: Environment manager.
        db: Database manager providing the registered domains.
    """
    if not is_enabled(env):
        return

    dns_dir = get_dns_dir(project_dir)
    dns_dir.mkdir(parents=True, exist_ok=True)

    domains = (
        db.get_domains()
        + [alias for alias, _ in db.get_aliases()]
        + [domain for domain, _ in db.get_proxies()]
        + soketi.get_domains(env)
    )
    _write_atomic(dns_dir / "Corefile", build_corefile(env.get("DNS_UPSTREAM") or DEFAULT_UPSTREAM))
    _write_atomic(dns_dir / "hosts", build_records(resolve_ip(env), domains))
