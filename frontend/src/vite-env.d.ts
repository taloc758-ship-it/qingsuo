/// <reference types="vite/client" />

interface Window {
  qingSuoWindow?: {
    minimize: () => Promise<void>
    toggleMaximize: () => Promise<void>
    hide: () => Promise<void>
  }
}
