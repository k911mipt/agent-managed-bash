import { ManagedBashPlugin } from "./managed-bash-plugin"
import { managedBashReleaseVersion } from "./release-version"

Object.defineProperty(ManagedBashPlugin, "managedBashReleaseVersion", {
  value: managedBashReleaseVersion,
})

export { ManagedBashPlugin }
