import { useSyncExternalStore } from 'react'
import '../styles/states.css'

function subscribe(callback: () => void) {
  window.addEventListener('online', callback)
  window.addEventListener('offline', callback)
  return () => {
    window.removeEventListener('online', callback)
    window.removeEventListener('offline', callback)
  }
}

/** Tracks navigator.onLine and the online/offline events. */
export function useOnlineStatus(): boolean {
  return useSyncExternalStore(subscribe, () => navigator.onLine, () => true)
}

/** Slim banner shown only while offline. Preview data is local, so actions still work (§4). */
export function OfflineBanner() {
  const online = useOnlineStatus()
  if (online) return null
  return <p className="offline-banner" role="status">You’re offline. This app uses browser-only storage; check the save status before leaving.</p>
}
