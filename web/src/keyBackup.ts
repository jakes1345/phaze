// PIN-encrypted backup of the NaCl identity keypair.
//
// We derive an AES-GCM key from the user's PIN via PBKDF2-SHA256, then
// encrypt a JSON-serialized keypair. The server stores the ciphertext +
// salt + iteration count opaquely — it can never decrypt without the PIN.
// PBKDF2 (rather than Argon2) keeps this in pure WebCrypto with no deps.

const ITERATIONS = 200_000
const SALT_BYTES = 16
const IV_BYTES = 12 // AES-GCM standard

function b64(b: Uint8Array): string {
  let s = ''
  for (const x of b) s += String.fromCharCode(x)
  return btoa(s)
}

function unb64(s: string): Uint8Array {
  const bin = atob(s)
  const u = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i)
  return u
}

function toBuf(u: Uint8Array): ArrayBuffer {
  const b = new ArrayBuffer(u.byteLength)
  new Uint8Array(b).set(u)
  return b
}

async function deriveKey(pin: string, salt: Uint8Array, iterations: number): Promise<CryptoKey> {
  const pinBytes = toBuf(new TextEncoder().encode(pin))
  const base = await crypto.subtle.importKey('raw', pinBytes, 'PBKDF2', false, ['deriveKey'])
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt: toBuf(salt), iterations, hash: 'SHA-256' },
    base,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  )
}

export interface KeyBackupBlob {
  ciphertext: string
  salt: string
  iterations: number
}

export async function encryptKeypair(
  publicKey: Uint8Array,
  secretKey: Uint8Array,
  pin: string,
): Promise<KeyBackupBlob> {
  if (pin.length < 4) throw new Error('PIN must be at least 4 characters')
  const salt = crypto.getRandomValues(new Uint8Array(SALT_BYTES))
  const key = await deriveKey(pin, salt, ITERATIONS)
  const iv = crypto.getRandomValues(new Uint8Array(IV_BYTES))
  const payload = JSON.stringify({ pub: b64(publicKey), sec: b64(secretKey) })
  const ct = new Uint8Array(
    await crypto.subtle.encrypt({ name: 'AES-GCM', iv: toBuf(iv) }, key, toBuf(new TextEncoder().encode(payload))),
  )
  const combined = new Uint8Array(iv.length + ct.length)
  combined.set(iv, 0)
  combined.set(ct, iv.length)
  return {
    ciphertext: b64(combined),
    salt: b64(salt),
    iterations: ITERATIONS,
  }
}

export async function decryptKeypair(
  blob: KeyBackupBlob,
  pin: string,
): Promise<{ publicKey: Uint8Array; secretKey: Uint8Array }> {
  const salt = unb64(blob.salt)
  const combined = unb64(blob.ciphertext)
  if (combined.length < IV_BYTES + 16) throw new Error('backup blob malformed')
  const iv = combined.slice(0, IV_BYTES)
  const ct = combined.slice(IV_BYTES)
  const key = await deriveKey(pin, salt, blob.iterations || ITERATIONS)
  let pt: ArrayBuffer
  try {
    pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: toBuf(iv) }, key, toBuf(ct))
  } catch {
    throw new Error('Incorrect PIN')
  }
  const json = JSON.parse(new TextDecoder().decode(pt)) as { pub: string; sec: string }
  const pub = unb64(json.pub)
  const sec = unb64(json.sec)
  if (pub.length !== 32 || sec.length !== 32) throw new Error('backup contained malformed keys')
  return { publicKey: pub, secretKey: sec }
}
