// What a route requires before it loads. Two sentences, stated once, so no
// route can ask a weaker question than its neighbour — which is what happened
// when each `beforeLoad` wrote its own: most of the admin tested whether a
// token STRING existed, which an expired session passes.
import { redirect } from '@tanstack/react-router'

import { session } from '~/lib/api'
import { base } from '~/lib/base'

// There has to be a session to act with. `session()` renews a bearer that has
// run down before it answers, so a route sends someone to sign in only when
// signing in is genuinely what is needed.
export async function requireSession(): Promise<void> {
  if (!await session()) throw redirect({ to: '/login' })
}

// Settings reach the whole process — schema, backups, logs, rate limits — and
// one process serves many orgs' Bases. So they ask a second question on top of
// the first. The server decides this for real; this only spares the round trip.
export async function requireSuperuser(): Promise<void> {
  await requireSession()
  if (!base.authStore.isSuperuser) throw redirect({ to: '/login' })
}
