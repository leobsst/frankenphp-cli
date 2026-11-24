import { Device } from '@capacitor/device'

/** Name of this phone in the dashboard's list of sessions (Remote access › Devices). */
export async function deviceName() {
  try {
    const info = await Device.getInfo()
    return `App FrankenManager · ${info.name || info.model}`
  } catch {
    return 'App FrankenManager'
  }
}
