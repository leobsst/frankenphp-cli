package dev.frankenmanager.mobile;

import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.net.ConnectivityManager;
import android.net.LinkAddress;
import android.net.LinkProperties;
import android.net.Network;
import android.os.Build;

import com.getcapacitor.JSObject;
import com.getcapacitor.PermissionState;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;
import com.getcapacitor.annotation.Permission;
import com.getcapacitor.annotation.PermissionCallback;

import java.net.Inet4Address;
import java.net.InetAddress;
import java.util.Arrays;

/**
 * Local network of the device (src/lib/localNetwork.ts):
 *
 * - its private IPv4 address on the current network (Wi-Fi, Ethernet) and the prefix length of that network, for the
 *   address placeholder of the instance form, read from the link properties of the system (ACCESS_NETWORK_STATE,
 *   granted at install): nothing is sent anywhere;
 * - the access to the local network itself, which Android 17 (API 37) blocks for an app targeting it until the user
 *   allows it (ACCESS_LOCAL_NETWORK, "Nearby devices"). Below, INTERNET grants it: the access is always granted.
 */
@CapacitorPlugin(
    name = "LocalNetwork",
    permissions = { @Permission(alias = LocalNetworkPlugin.ALIAS, strings = { LocalNetworkPlugin.ACCESS_LOCAL_NETWORK }) }
)
public class LocalNetworkPlugin extends Plugin {
    static final String ALIAS = "localNetwork";
    /** Manifest.permission.ACCESS_LOCAL_NETWORK, absent from the SDKs before 37. */
    static final String ACCESS_LOCAL_NETWORK = "android.permission.ACCESS_LOCAL_NETWORK";
    private static final int ANDROID_17 = 37;

    /** Resolves `{ ip, prefix }`, or `{}` without a private IPv4 address (cellular data only). */
    @PluginMethod
    public void ipv4(PluginCall call) {
        JSObject result = new JSObject();
        ConnectivityManager cm = getContext().getSystemService(ConnectivityManager.class);
        Network network = cm == null ? null : cm.getActiveNetwork();
        LinkProperties link = network == null ? null : cm.getLinkProperties(network);
        if (link != null) {
            for (LinkAddress la : link.getLinkAddresses()) {
                InetAddress address = la.getAddress();
                // Private networks only (10/8, 172.16/12, 192.168/16).
                if (address instanceof Inet4Address && address.isSiteLocalAddress()) {
                    result.put("ip", address.getHostAddress());
                    result.put("prefix", la.getPrefixLength());
                    break;
                }
            }
        }
        call.resolve(result);
    }

    /** Resolves `{ access }`: `granted`, `denied`, `prompt` or `prompt-with-rationale`. */
    @PluginMethod
    public void access(PluginCall call) {
        resolveAccess(call);
    }

    /** Asks for the local network when Android requires it (system prompt), then resolves `{ access }`. */
    @PluginMethod
    public void requestAccess(PluginCall call) {
        if (!permissionRequired() || getPermissionState(ALIAS) == PermissionState.GRANTED) {
            resolveAccess(call);
            return;
        }
        // Without the declaration, Android would refuse at once and silently: app/build.gradle adds it from
        // targetSdkVersion 37.
        if (!declared()) {
            call.reject(ACCESS_LOCAL_NETWORK + " is missing from the merged AndroidManifest.xml (see app/build.gradle).");
            return;
        }
        requestPermissionForAlias(ALIAS, call, "accessResult");
    }

    @PermissionCallback
    private void accessResult(PluginCall call) {
        resolveAccess(call);
    }

    private void resolveAccess(PluginCall call) {
        JSObject result = new JSObject();
        result.put("access", permissionRequired() ? getPermissionState(ALIAS).toString() : PermissionState.GRANTED.toString());
        call.resolve(result);
    }

    /** Android 17 or later, with an app targeting it: the local network needs the user's permission. */
    private boolean permissionRequired() {
        return Build.VERSION.SDK_INT >= ANDROID_17 && getContext().getApplicationInfo().targetSdkVersion >= ANDROID_17;
    }

    /** The permission is in the manifest of the installed app. */
    private boolean declared() {
        try {
            PackageManager pm = getContext().getPackageManager();
            PackageInfo info = pm.getPackageInfo(getContext().getPackageName(), PackageManager.GET_PERMISSIONS);
            return info.requestedPermissions != null && Arrays.asList(info.requestedPermissions).contains(ACCESS_LOCAL_NETWORK);
        } catch (PackageManager.NameNotFoundException e) {
            return false;
        }
    }
}
