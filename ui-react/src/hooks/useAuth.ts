import { useEffect, useState } from 'react'
import { clearAuth, getRecord, getToken, onAuthChange } from '~/lib/api'

export function useAuth() {
  const [token, setToken] = useState(getToken)
  const [record, setRecord] = useState(getRecord)

  useEffect(() => {
    return onAuthChange(() => {
      setToken(getToken())
      setRecord(getRecord())
    })
  }, [])

  return {
    token,
    record,
    isAuthenticated: Boolean(token && record),
    signOut: clearAuth,
  }
}
