# FrankenManager app — Privacy policy

_Last updated: November 24, 2026_

FrankenManager is an open source app that lets you manage the FrankenManager instances you run on your own machines. It is built so that your data never leaves your devices and your machines.

## What the app does with your data

- **No collection.** The app has no account, no analytics, no advertising, no crash reporting and no third-party tracking. Nothing is sent to the developer or to any third party.
- **Your instances only.** The app talks only to the FrankenManager instances you add (their address on your network, or a public address you set up), over HTTPS. What it shows — projects, containers, logs, databases, storage — is read from those instances and stays between them and your device.
- **On your device.** The list of your instances (name and address) is kept in the app's storage. Session tokens are kept in the iOS Keychain (this device only, never synced) or the Android Keystore. Uninstalling the app deletes them; you can also revoke a device from the dashboard of the instance at any time.
- **Camera.** The camera is used only to scan the pairing QR code shown by the dashboard. No image is stored or sent.
- **Local network.** Access to the local network is used only to reach your instances.

## Contact

Questions or requests: open an issue at https://github.com/leobsst/frankenphp-cli/issues.

---

## Français

_Dernière mise à jour : 24 novembre 2026_

FrankenManager est une app open source qui vous permet de piloter les instances FrankenManager que vous faites tourner sur vos propres machines. Elle est conçue pour que vos données ne quittent jamais vos appareils et vos machines.

### Ce que l’app fait de vos données

- **Aucune collecte.** L’app n’a ni compte, ni statistiques, ni publicité, ni rapport de plantage, ni pistage par un tiers. Rien n’est envoyé au développeur ni à un tiers.
- **Seulement vos instances.** L’app ne parle qu’aux instances FrankenManager que vous ajoutez (leur adresse sur votre réseau, ou une adresse publique que vous avez configurée), en HTTPS. Ce qu’elle affiche — projets, conteneurs, journaux, bases de données, stockage — est lu sur ces instances et reste entre elles et votre appareil.
- **Sur votre appareil.** La liste de vos instances (nom et adresse) est gardée dans le stockage de l’app. Les jetons de session sont gardés dans le trousseau d’iOS (cet appareil uniquement, jamais synchronisés) ou dans le Keystore d’Android. Désinstaller l’app les supprime ; vous pouvez aussi déconnecter un appareil depuis le dashboard de l’instance à tout moment.
- **Appareil photo.** L’appareil photo sert uniquement à scanner le QR code d’appairage affiché par le dashboard. Aucune image n’est gardée ni envoyée.
- **Réseau local.** L’accès au réseau local sert uniquement à joindre vos instances.

### Contact

Questions ou demandes : ouvrez une issue sur https://github.com/leobsst/frankenphp-cli/issues.
