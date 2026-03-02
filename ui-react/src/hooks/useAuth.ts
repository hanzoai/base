import { useEffect, useState } from 'react';

import { base } from '~/lib/base';

// Expose auth state as a reactive hook. `authStore` is the SDK's source of
// truth; we subscribe and re-render on change.
export function useAuth() {
  const [token, setToken] = useState(base.authStore.token);
  const [record, setRecord] = useState(base.authStore.record);

  useEffect(() => {
    const unsub = base.authStore.onChange(() => {
      setToken(base.authStore.token);
      setRecord(base.authStore.record);
    });
    return () => unsub();
  }, []);

  return {
    token,
    record,
    isAuthenticated: Boolean(token && record),
    signOut: () => base.authStore.clear(),
  };
}
