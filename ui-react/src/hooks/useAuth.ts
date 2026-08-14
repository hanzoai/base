import { useEffect, useState } from 'react'
import { clearAuth, getRecord, getToken, onAuthChange } from '~/lib/api'

export function useAuth() {
  const [token, setToken] = useState(getToken)
  const [record, setRecord] = useState(getRecord)

  useEffect(() => {
    const sync = () => {
      setToken(getToken())
      setRecord(getRecord())
    }
    const stop = onAuthChange(sync)
    // The session lives in localStorage, which every tab on this origin shares.
    // A `storage` event is how this tab hears that another one signed in — the
    // tab that did the signing is not the tab that needs to know.
    window.addEventListener('storage', sync)
    return () => {
      stop()
      window.removeEventListener('storage', sync)
    }
  }, [])

  return {
    token,
    record,
    isAuthenticated: Boolean(token && record),
    signOut: clearAuth,
  }
}
