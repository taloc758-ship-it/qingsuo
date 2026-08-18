/// <reference types="vite/client" />

interface AutoLaunchSettings {
  supported: boolean
  enabled: boolean
}

interface AppIconSettings {
  selected: 'orbit' | 'shield' | 'prism' | 'pulse' | 'knot'
  variants: Array<'orbit' | 'shield' | 'prism' | 'pulse' | 'knot'>
}

interface Window {
  qingSuoWindow?: {
    minimize: () => Promise<void>
    toggleMaximize: () => Promise<void>
    hide: () => Promise<void>
    getAutoLaunchSettings: () => Promise<AutoLaunchSettings>
    setAutoLaunchEnabled: (enabled: boolean) => Promise<AutoLaunchSettings>
    getIconSettings: () => Promise<AppIconSettings>
    setIconVariant: (variant: AppIconSettings['selected']) => Promise<AppIconSettings>
  }
}
