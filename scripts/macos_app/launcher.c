/*
 * Main executable of FrankenManager.app, the macOS app shipped inside the
 * onedir bundle:
 *
 *     frankenmanager/
 *         frankenmanager                      <- the real CLI
 *         FrankenManager.app/Contents/MacOS/
 *             frankenmanager-agent            <- this program
 *
 * The background items started by FrankenManager (the LAN DNS watcher's
 * LaunchAgent) run through this launcher so that macOS attributes them to
 * the app and shows "FrankenManager" in Login Items, instead of the name on
 * the code signing certificate. It only replaces itself with the CLI, with
 * the same arguments.
 */

#include <libgen.h>
#include <limits.h>
#include <mach-o/dyld.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char *argv[]) {
    (void)argc;

    char raw[PATH_MAX];
    uint32_t size = sizeof(raw);
    if (_NSGetExecutablePath(raw, &size) != 0) {
        fprintf(stderr, "frankenmanager-agent: executable path too long\n");
        return 1;
    }

    char path[PATH_MAX];
    if (realpath(raw, path) == NULL) {
        perror("frankenmanager-agent: realpath");
        return 1;
    }

    /* .../MacOS -> .../Contents -> .../FrankenManager.app -> bundle dir */
    char *dir = path;
    for (int i = 0; i < 4; i++) {
        dir = dirname(dir);
        if (dir == NULL) {
            perror("frankenmanager-agent: dirname");
            return 1;
        }
        /* dirname may return internal storage: copy it back for the next round */
        memmove(path, dir, strlen(dir) + 1);
        dir = path;
    }

    char cli[PATH_MAX];
    if (snprintf(cli, sizeof(cli), "%s/frankenmanager", dir) >= (int)sizeof(cli)) {
        fprintf(stderr, "frankenmanager-agent: CLI path too long\n");
        return 1;
    }

    argv[0] = cli;
    execv(cli, argv);
    perror("frankenmanager-agent: execv");
    return 127;
}
