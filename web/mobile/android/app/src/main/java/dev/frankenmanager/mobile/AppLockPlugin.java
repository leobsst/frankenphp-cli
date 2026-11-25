package dev.frankenmanager.mobile;

import android.app.KeyguardManager;
import android.content.Context;
import android.hardware.biometrics.BiometricManager;
import android.hardware.biometrics.BiometricPrompt;
import android.os.Build;
import android.os.CancellationSignal;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

/**
 * Lock of the app (src/lib/appLock.ts): fingerprint or face unlock, with the screen lock (PIN, pattern, password) as
 * fallback, through the system BiometricPrompt (Android 9 and later; without it, the lock is not offered).
 */
@CapacitorPlugin(name = "AppLock")
public class AppLockPlugin extends Plugin {
    /** Android 11+: any biometry the system accepts for unlocking, or the screen lock. */
    private static final int AUTHENTICATORS =
        BiometricManager.Authenticators.BIOMETRIC_WEAK | BiometricManager.Authenticators.DEVICE_CREDENTIAL;

    /** Resolves `{ available, biometry }`: whether the owner can authenticate, and `biometric` or `none`. */
    @PluginMethod
    public void status(PluginCall call) {
        Context ctx = getContext();
        boolean available = false;
        boolean biometric = false;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            BiometricManager bm = ctx.getSystemService(BiometricManager.class);
            available = bm != null && bm.canAuthenticate(AUTHENTICATORS) == BiometricManager.BIOMETRIC_SUCCESS;
            biometric = bm != null && bm.canAuthenticate(BiometricManager.Authenticators.BIOMETRIC_WEAK) == BiometricManager.BIOMETRIC_SUCCESS;
        } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            KeyguardManager km = ctx.getSystemService(KeyguardManager.class);
            available = km != null && km.isDeviceSecure();
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                BiometricManager bm = ctx.getSystemService(BiometricManager.class);
                biometric = bm != null && bm.canAuthenticate() == BiometricManager.BIOMETRIC_SUCCESS;
            }
            // Android 9 cannot fall back to the screen lock: biometry is needed.
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) available = available && biometric;
        }
        JSObject result = new JSObject();
        result.put("available", available);
        result.put("biometry", biometric ? "biometric" : "none");
        call.resolve(result);
    }

    /** Asks the owner to authenticate; rejects with the code `cancelled`, `unavailable` or `failed`. */
    @PluginMethod
    public void authenticate(PluginCall call) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.P) {
            call.reject("Biometric authentication needs Android 9 or later.", "unavailable");
            return;
        }
        String title = call.getString("reason", "FrankenManager");
        String cancel = call.getString("cancelTitle", "Cancel");
        getActivity().runOnUiThread(() -> {
            Context ctx = getContext();
            BiometricPrompt.Builder builder = new BiometricPrompt.Builder(ctx).setTitle(title);
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                builder.setAllowedAuthenticators(AUTHENTICATORS);
            } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                builder.setDeviceCredentialAllowed(true);
            } else {
                builder.setNegativeButton(cancel, ctx.getMainExecutor(), (dialog, which) -> call.reject("Cancelled.", "cancelled"));
            }
            builder.build().authenticate(new CancellationSignal(), ctx.getMainExecutor(), new BiometricPrompt.AuthenticationCallback() {
                @Override
                public void onAuthenticationSucceeded(BiometricPrompt.AuthenticationResult result) {
                    call.resolve();
                }

                @Override
                public void onAuthenticationError(int errorCode, CharSequence errString) {
                    String code;
                    switch (errorCode) {
                        case BiometricPrompt.BIOMETRIC_ERROR_USER_CANCELED:
                        case BiometricPrompt.BIOMETRIC_ERROR_CANCELED:
                            code = "cancelled";
                            break;
                        case BiometricPrompt.BIOMETRIC_ERROR_HW_NOT_PRESENT:
                        case BiometricPrompt.BIOMETRIC_ERROR_HW_UNAVAILABLE:
                        case BiometricPrompt.BIOMETRIC_ERROR_NO_BIOMETRICS:
                            code = "unavailable";
                            break;
                        default:
                            code = "failed";
                    }
                    call.reject(String.valueOf(errString), code);
                }
                // onAuthenticationFailed (one attempt not recognised): the prompt stays and lets the owner try again.
            });
        });
    }
}
