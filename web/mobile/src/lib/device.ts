import { Device } from '@capacitor/device'
import { t } from '@app/i18n'

/** Name of the app, as a client of the daemon. */
export const deviceLabel = () => t('mobile.device.name')

/** Name of this phone in the dashboard's list of sessions (Remote access › Devices). */
export async function deviceName() {
  try {
    const info = await Device.getInfo()
    return t('mobile.device.nameWithModel', { device: info.name || info.model })
  } catch {
    return deviceLabel()
  }
}
