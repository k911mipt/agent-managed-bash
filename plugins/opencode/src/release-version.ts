declare const __MANAGED_BASH_RELEASE_VERSION__: string

export const managedBashReleaseVersion =
  typeof __MANAGED_BASH_RELEASE_VERSION__ === "string" ? __MANAGED_BASH_RELEASE_VERSION__ : "dev"
