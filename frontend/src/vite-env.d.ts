/// <reference types="vite/client" />

interface AutoLaunchSettings {
  supported: boolean
  enabled: boolean
}

interface Window {
  qingSuoWindow?: {
    minimize: () => Promise<void>
    toggleMaximize: () => Promise<void>
    hide: () => Promise<void>
    getAutoLaunchSettings: () => Promise<AutoLaunchSettings>
    setAutoLaunchEnabled: (enabled: boolean) => Promise<AutoLaunchSettings>
  }
}
