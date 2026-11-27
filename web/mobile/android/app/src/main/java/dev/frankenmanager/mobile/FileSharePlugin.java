package dev.frankenmanager.mobile;

import android.content.ClipData;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;

import androidx.core.content.FileProvider;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;

/**
 * Share sheet of a text file (src/lib/share.ts): the WebView ignores the downloads of the page (the logs export), so
 * the file is written to the cache and handed to the system share sheet through the FileProvider of the app
 * (AndroidManifest.xml, res/xml/file_paths.xml).
 */
@CapacitorPlugin(name = "FileShare")
public class FileSharePlugin extends Plugin {
    /** Writes `text` as the file `name` and shows the share sheet. Resolves `{}`: Android does not tell the outcome. */
    @PluginMethod
    public void shareText(PluginCall call) {
        Context ctx = getContext();
        // The file name only: no directory taken from the page.
        String name = new File(call.getString("name", "file.txt")).getName();
        String text = call.getString("text", "");
        File dir = new File(ctx.getCacheDir(), "shared");
        File file = new File(dir, name);
        try {
            if (!dir.isDirectory() && !dir.mkdirs()) throw new IOException("Cannot create " + dir);
            try (OutputStream out = new FileOutputStream(file)) {
                out.write(text.getBytes(StandardCharsets.UTF_8));
            }
        } catch (IOException e) {
            call.reject(e.getMessage());
            return;
        }
        Uri uri = FileProvider.getUriForFile(ctx, ctx.getPackageName() + ".fileprovider", file);
        Intent send = new Intent(Intent.ACTION_SEND)
            .setType("text/plain")
            .putExtra(Intent.EXTRA_STREAM, uri)
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        // The chooser passes the read permission on to the chosen app through the clip data.
        send.setClipData(ClipData.newRawUri(name, uri));
        getActivity().startActivity(Intent.createChooser(send, null));
        call.resolve(new JSObject());
    }
}
