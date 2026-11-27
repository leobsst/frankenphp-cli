package dev.frankenmanager.mobile;

import android.os.Bundle;

import com.getcapacitor.BridgeActivity;

/** Capacitor activity of the app: registers the plugins written in the app itself (outside npm packages). */
public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        registerPlugin(SecureStorePlugin.class);
        registerPlugin(AppLockPlugin.class);
        registerPlugin(LocalNetworkPlugin.class);
        registerPlugin(FileSharePlugin.class);
        super.onCreate(savedInstanceState);
    }
}
