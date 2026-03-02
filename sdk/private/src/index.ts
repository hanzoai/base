// @hanzo/private — per-user encrypted blob store SDK.
//
// One API, three backends:
//   * LocalBackend: PUT/GET/LIST/DELETE /v1/base/private/{tag}
//   * ChainBackend: PrivateStore.sol on any EVM
//   * IpfsBackend:  store ciphertext on IPFS, index CID locally
//
// Clients are responsible for encryption. This SDK carries ciphertext and
// doesn't touch plaintext. Pair it with a passkey-derived key + age/X-Wing
// for end-to-end PQ-safe privacy.
//
//   const store = new PrivateStore({ backend: 'local', baseUrl: '/v1/base' })
//   await store.put('watchlist', ciphertext)       // Uint8Array
//   const ct = await store.get('watchlist')
//   await store.list()                             // [{tag, size, updatedAt}]
//   await store.delete('watchlist')

export interface PrivateBackend {
  put(tag: string, ct: Uint8Array): Promise<{ tag: string; size: number; updatedAt: number }>
  get(tag: string): Promise<Uint8Array>
  list(): Promise<PrivateListItem[]>
  delete(tag: string): Promise<void>
}

export interface PrivateListItem {
  tag: string
  size: number
  updatedAt: number
}

// ---------- Local (Base /private) ----------

export interface LocalBackendOptions {
  /** Base URL for the Base API group. Defaults to '/api'. */
  baseUrl?: string
  /** Called for each request to attach auth (e.g. Bearer JWT). */
  authHeader?: () => string | undefined
  /** Custom fetch for non-browser environments. */
  fetchImpl?: typeof fetch
}

export class LocalBackend implements PrivateBackend {
  private base: string
  private authHeader?: () => string | undefined
  private fetchImpl: typeof fetch

  constructor(opts: LocalBackendOptions = {}) {
    this.base = (opts.baseUrl ?? '/api').replace(/\/$/, '')
    this.authHeader = opts.authHeader
    this.fetchImpl = opts.fetchImpl ?? globalThis.fetch.bind(globalThis)
  }

  private headers(extra?: Record<string, string>): HeadersInit {
    const h: Record<string, string> = { ...(extra ?? {}) }
    const auth = this.authHeader?.()
    if (auth) h['Authorization'] = auth
    return h
  }

  async put(tag: string, ct: Uint8Array): Promise<{ tag: string; size: number; updatedAt: number }> {
    if (ct.length === 0) throw new Error('ciphertext must be non-empty; use delete() to remove')
    // Copy into a fresh ArrayBuffer so TS narrows away SharedArrayBuffer.
    const buf = new ArrayBuffer(ct.byteLength)
    new Uint8Array(buf).set(ct)
    const body = new Blob([buf])
    const res = await this.fetchImpl(`${this.base}/private/${encodeURIComponent(tag)}`, {
      method: 'PUT',
      body,
      headers: this.headers({ 'Content-Type': 'application/octet-stream' }),
    })
    if (!res.ok) throw new PrivateError(res.status, await res.text())
    const j = (await res.json()) as { tag: string; size: number; updated_at: number }
    return { tag: j.tag, size: j.size, updatedAt: j.updated_at }
  }

  async get(tag: string): Promise<Uint8Array> {
    const res = await this.fetchImpl(`${this.base}/private/${encodeURIComponent(tag)}`, {
      method: 'GET',
      headers: this.headers(),
    })
    if (res.status === 404) throw new PrivateError(404, 'not found')
    if (!res.ok) throw new PrivateError(res.status, await res.text())
    const buf = await res.arrayBuffer()
    return new Uint8Array(buf)
  }

  async list(): Promise<PrivateListItem[]> {
    const res = await this.fetchImpl(`${this.base}/private`, {
      method: 'GET',
      headers: this.headers(),
    })
    if (!res.ok) throw new PrivateError(res.status, await res.text())
    const j = (await res.json()) as { items: Array<{ tag: string; size: number; updated_at: number }> }
    return j.items.map((r) => ({ tag: r.tag, size: r.size, updatedAt: r.updated_at }))
  }

  async delete(tag: string): Promise<void> {
    const res = await this.fetchImpl(`${this.base}/private/${encodeURIComponent(tag)}`, {
      method: 'DELETE',
      headers: this.headers(),
    })
    if (!res.ok && res.status !== 204) throw new PrivateError(res.status, await res.text())
  }
}

// ---------- Chain (PrivateStore.sol) ----------

/** Minimal viem-shaped interface — we don't import viem to keep this a
 *  peer dep. Users pass in their already-initialised clients. */
export interface ChainBackendOptions {
  /** Contract address on the target chain. */
  address: `0x${string}`
  /** viem PublicClient-like object: readContract(). */
  publicClient: {
    readContract: (args: {
      address: `0x${string}`
      abi: readonly unknown[]
      functionName: string
      args: readonly unknown[]
    }) => Promise<unknown>
  }
  /** viem WalletClient-like object: writeContract(), account. */
  walletClient: {
    account: { address: `0x${string}` }
    writeContract: (args: {
      address: `0x${string}`
      abi: readonly unknown[]
      functionName: string
      args: readonly unknown[]
    }) => Promise<`0x${string}`>
  }
}

export const privateStoreAbi = [
  {
    type: 'function',
    name: 'put',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'tagHash', type: 'bytes32' },
      { name: 'ct', type: 'bytes' },
    ],
    outputs: [],
  },
  {
    type: 'function',
    name: 'get',
    stateMutability: 'view',
    inputs: [
      { name: 'owner', type: 'address' },
      { name: 'tagHash', type: 'bytes32' },
    ],
    outputs: [{ name: '', type: 'bytes' }],
  },
  {
    type: 'function',
    name: 'updatedAt',
    stateMutability: 'view',
    inputs: [
      { name: 'owner', type: 'address' },
      { name: 'tagHash', type: 'bytes32' },
    ],
    outputs: [{ name: '', type: 'uint64' }],
  },
  {
    type: 'function',
    name: 'del',
    stateMutability: 'nonpayable',
    inputs: [{ name: 'tagHash', type: 'bytes32' }],
    outputs: [],
  },
] as const

/** Client-side tag hashing. The chain never sees the raw tag string. */
export async function hashTag(tag: string): Promise<`0x${string}`> {
  const data = new TextEncoder().encode(tag)
  const digest = await crypto.subtle.digest('SHA-256', data)
  const bytes = new Uint8Array(digest)
  return ('0x' + Array.from(bytes).map((b) => b.toString(16).padStart(2, '0')).join('')) as `0x${string}`
}

export class ChainBackend implements PrivateBackend {
  constructor(private opts: ChainBackendOptions) {}

  async put(tag: string, ct: Uint8Array): Promise<{ tag: string; size: number; updatedAt: number }> {
    if (ct.length === 0) throw new Error('ciphertext must be non-empty; use delete() to remove')
    const tagHash = await hashTag(tag)
    const ctHex = ('0x' + Array.from(ct).map((b) => b.toString(16).padStart(2, '0')).join('')) as `0x${string}`
    await this.opts.walletClient.writeContract({
      address: this.opts.address,
      abi: privateStoreAbi,
      functionName: 'put',
      args: [tagHash, ctHex],
    })
    // Contract returns no value; read timestamp back.
    const ts = (await this.opts.publicClient.readContract({
      address: this.opts.address,
      abi: privateStoreAbi,
      functionName: 'updatedAt',
      args: [this.opts.walletClient.account.address, tagHash],
    })) as bigint
    return { tag, size: ct.length, updatedAt: Number(ts) * 1000 }
  }

  async get(tag: string): Promise<Uint8Array> {
    const tagHash = await hashTag(tag)
    const result = (await this.opts.publicClient.readContract({
      address: this.opts.address,
      abi: privateStoreAbi,
      functionName: 'get',
      args: [this.opts.walletClient.account.address, tagHash],
    })) as `0x${string}`
    return hexToBytes(result)
  }

  async list(): Promise<PrivateListItem[]> {
    // On-chain listing requires indexing Put events off-chain. By design
    // this backend doesn't enumerate — callers should keep a local index
    // (e.g. via LocalBackend with the CIDs/tagHashes as small entries).
    throw new Error(
      'ChainBackend.list() is not supported on-chain by design. ' +
        'Index Put events off-chain or use a companion LocalBackend for enumeration.',
    )
  }

  async delete(tag: string): Promise<void> {
    const tagHash = await hashTag(tag)
    await this.opts.walletClient.writeContract({
      address: this.opts.address,
      abi: privateStoreAbi,
      functionName: 'del',
      args: [tagHash],
    })
  }
}

// ---------- IPFS ----------

export interface IpfsBackendOptions {
  /** HTTP API endpoint for the IPFS node (e.g. http://localhost:5001). */
  apiUrl: string
  /** Companion index for enumeration — normally a LocalBackend. */
  index: PrivateBackend
  /** Custom fetch. */
  fetchImpl?: typeof fetch
}

/** Write ciphertext to IPFS and track the CID in a companion index.
 *  The ciphertext is opaque — IPFS nodes can't decrypt. */
export class IpfsBackend implements PrivateBackend {
  private api: string
  private fetchImpl: typeof fetch
  private index: PrivateBackend

  constructor(opts: IpfsBackendOptions) {
    this.api = opts.apiUrl.replace(/\/$/, '')
    this.fetchImpl = opts.fetchImpl ?? globalThis.fetch.bind(globalThis)
    this.index = opts.index
  }

  async put(tag: string, ct: Uint8Array): Promise<{ tag: string; size: number; updatedAt: number }> {
    const form = new FormData()
    const buf = new ArrayBuffer(ct.byteLength)
    new Uint8Array(buf).set(ct)
    form.append('file', new Blob([buf]))
    const res = await this.fetchImpl(`${this.api}/api/v0/add?pin=true`, {
      method: 'POST',
      body: form,
    })
    if (!res.ok) throw new PrivateError(res.status, await res.text())
    const j = (await res.json()) as { Hash: string }
    const cid = j.Hash
    await this.index.put(tag, new TextEncoder().encode(cid))
    return { tag, size: ct.length, updatedAt: Date.now() }
  }

  async get(tag: string): Promise<Uint8Array> {
    const cidBytes = await this.index.get(tag)
    const cid = new TextDecoder().decode(cidBytes)
    const res = await this.fetchImpl(`${this.api}/api/v0/cat?arg=${encodeURIComponent(cid)}`, {
      method: 'POST',
    })
    if (!res.ok) throw new PrivateError(res.status, await res.text())
    return new Uint8Array(await res.arrayBuffer())
  }

  async list(): Promise<PrivateListItem[]> {
    return this.index.list()
  }

  async delete(tag: string): Promise<void> {
    // Unpin the CID we recorded, then remove from the index.
    try {
      const cidBytes = await this.index.get(tag)
      const cid = new TextDecoder().decode(cidBytes)
      await this.fetchImpl(`${this.api}/api/v0/pin/rm?arg=${encodeURIComponent(cid)}`, { method: 'POST' })
    } catch {
      // Indifferent to unpin failure — the index delete is what matters.
    }
    await this.index.delete(tag)
  }
}

// ---------- Errors ----------

export class PrivateError extends Error {
  constructor(public status: number, public body: string) {
    super(`PrivateStore error: ${status} ${body}`)
  }
}

// ---------- Factory ----------

export type PrivateStoreOptions =
  | ({ backend: 'local' } & LocalBackendOptions)
  | ({ backend: 'chain' } & ChainBackendOptions)
  | ({ backend: 'ipfs' } & IpfsBackendOptions)

export function createPrivateStore(opts: PrivateStoreOptions): PrivateBackend {
  switch (opts.backend) {
    case 'local':
      return new LocalBackend(opts)
    case 'chain':
      return new ChainBackend(opts)
    case 'ipfs':
      return new IpfsBackend(opts)
  }
}

// ---------- Utils ----------

function hexToBytes(hex: `0x${string}`): Uint8Array {
  const s = hex.slice(2)
  const out = new Uint8Array(s.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16)
  return out
}
