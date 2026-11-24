package dev.frankenmanager.mobile;

import android.content.Context;
import android.content.SharedPreferences;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Base64;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/**
 * Secret storage backed by the Android Keystore, for the session tokens of the instances (src/lib/secureStore.ts).
 *
 * An AES-256 key is generated inside the Keystore (it never leaves it, hardware-backed when the device allows it);
 * each value is encrypted with it (AES/GCM) and the ciphertext kept in a private SharedPreferences file. That file is
 * left out of backups (res/xml/backup_rules.xml, data_extraction_rules.xml): restored elsewhere, it could not be
 * decrypted anyway, since the key stays in this device's Keystore. A value that cannot be decrypted any more (key
 * lost after a reset) reads as missing and is dropped.
 */
@CapacitorPlugin(name = "SecureStore")
public class SecureStorePlugin extends Plugin {
    private static final String KEYSTORE = "AndroidKeyStore";
    private static final String KEY_ALIAS = "frankenmanager.secure-store";
    private static final String PREFS = "frankenmanager_secure_store";
    private static final String TRANSFORMATION = "AES/GCM/NoPadding";
    private static final int IV_LENGTH = 12;
    private static final int TAG_LENGTH = 128;

    private SharedPreferences prefs() {
        return getContext().getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    private SecretKey key() throws Exception {
        KeyStore ks = KeyStore.getInstance(KEYSTORE);
        ks.load(null);
        KeyStore.Entry entry = ks.getEntry(KEY_ALIAS, null);
        if (entry instanceof KeyStore.SecretKeyEntry) {
            return ((KeyStore.SecretKeyEntry) entry).getSecretKey();
        }
        KeyGenerator generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE);
        generator.init(new KeyGenParameterSpec.Builder(KEY_ALIAS, KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setKeySize(256)
            .build());
        return generator.generateKey();
    }

    private String encrypt(String value) throws Exception {
        Cipher cipher = Cipher.getInstance(TRANSFORMATION);
        cipher.init(Cipher.ENCRYPT_MODE, key());
        byte[] iv = cipher.getIV();
        byte[] sealed = cipher.doFinal(value.getBytes(StandardCharsets.UTF_8));
        ByteBuffer out = ByteBuffer.allocate(iv.length + sealed.length).put(iv).put(sealed);
        return Base64.encodeToString(out.array(), Base64.NO_WRAP);
    }

    private String decrypt(String stored) throws Exception {
        byte[] raw = Base64.decode(stored, Base64.NO_WRAP);
        if (raw.length <= IV_LENGTH) throw new IllegalArgumentException("Truncated value");
        Cipher cipher = Cipher.getInstance(TRANSFORMATION);
        cipher.init(Cipher.DECRYPT_MODE, key(), new GCMParameterSpec(TAG_LENGTH, raw, 0, IV_LENGTH));
        byte[] plain = cipher.doFinal(raw, IV_LENGTH, raw.length - IV_LENGTH);
        return new String(plain, StandardCharsets.UTF_8);
    }

    /** Resolves { value }, null when the key is not stored (or no longer readable). */
    @PluginMethod
    public void get(PluginCall call) {
        String key = call.getString("key");
        if (key == null || key.isEmpty()) {
            call.reject("Missing key.");
            return;
        }
        JSObject ret = new JSObject();
        String stored = prefs().getString(key, null);
        if (stored == null) {
            ret.put("value", JSObject.NULL);
            call.resolve(ret);
            return;
        }
        try {
            ret.put("value", decrypt(stored));
        } catch (Exception e) {
            prefs().edit().remove(key).apply();
            ret.put("value", JSObject.NULL);
        }
        call.resolve(ret);
    }

    /** Stores value under key, replacing a previous value. */
    @PluginMethod
    public void set(PluginCall call) {
        String key = call.getString("key");
        String value = call.getString("value");
        if (key == null || key.isEmpty() || value == null) {
            call.reject("Missing key or value.");
            return;
        }
        try {
            if (!prefs().edit().putString(key, encrypt(value)).commit()) {
                call.reject("Secure store write failed.");
                return;
            }
            call.resolve();
        } catch (Exception e) {
            call.reject("Secure store write failed.", e);
        }
    }

    /** Deletes key; no error when it is not stored. */
    @PluginMethod
    public void remove(PluginCall call) {
        String key = call.getString("key");
        if (key == null || key.isEmpty()) {
            call.reject("Missing key.");
            return;
        }
        prefs().edit().remove(key).commit();
        call.resolve();
    }
}
