// AssemblyScript fixture for the Base extension benchmark harness.
//
// Host-guest ABI (Base wasmvm calling convention):
//   __base_alloc(size: i32) -> i32             host -> guest scratch
//   __base_free(ptr: i32, size: i32)           host releases scratch
//   validate(ptr: i32, len: i32) -> i64        (resultPtr << 32) | resultLen
//
// The host writes a JSON payload at ptr/len, calls validate, then reads
// resultLen bytes at resultPtr (UTF-8 JSON) and frees them via __base_free.
//
// Shared semantics across all 5 runtime fixtures (canonical reference:
// goja-js/validate.js):
//   in:  {"email":"Foo@Example.COM ","age":25}
//   ok:  {"ok":true,"email":"foo@example.com","age":25}
//   err: {"ok":false,"error":"input must be an object"}
//   err: {"ok":false,"error":"email required"}
//   err: {"ok":false,"error":"age out of range"}
//   err: {"ok":false,"error":"email shape"}
//   err: {"ok":false,"error":"email domain"}

// --- memory exports ---------------------------------------------------------

// Allocate `size` bytes of guest linear memory and return its raw address.
// Uses the AS runtime's __new with ArrayBuffer id so the GC roots the
// region until the host calls __base_free.
export function __base_alloc(size: i32): i32 {
  const ptr = changetype<i32>(__new(size, idof<ArrayBuffer>()));
  __pin(ptr);
  return ptr;
}

// Release a previously allocated region. AssemblyScript's minimal runtime
// reference-counts via __pin/__unpin; size is unused but kept in the
// signature so the host doesn't need to remember whether the runtime cares.
export function __base_free(ptr: i32, size: i32): void {
  __unpin(ptr);
}

// --- helpers ----------------------------------------------------------------

function readUtf8(ptr: i32, len: i32): string {
  return String.UTF8.decodeUnsafe(ptr, len, false);
}

// Encode a string as UTF-8 and pin the buffer so it survives until the
// host calls __base_free. Returns (ptr << 32) | len.
function pack(s: string): i64 {
  const buf = String.UTF8.encode(s, false);
  const ptr = changetype<i32>(buf);
  __pin(ptr);
  const len = buf.byteLength;
  return (i64(ptr) << 32) | i64(len);
}

// --- tolerant scraper -------------------------------------------------------
//
// AssemblyScript has no JSON parser in the stdlib and we don't want a wasm
// runtime dependency just to read two fields. The benchmark harness only
// ever sends {"email":<string>,"age":<number>}, so a tolerant key-lookup
// is correct (and small enough to keep the .wasm under 15 KB).

function findValueStart(src: string, key: string): i32 {
  const needle = '"' + key + '"';
  let idx = src.indexOf(needle);
  if (idx < 0) return -1;
  idx += needle.length;
  while (idx < src.length) {
    const c = src.charCodeAt(idx);
    if (c == 0x3a /* : */) { idx++; break; }
    if (c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d) { idx++; continue; }
    return -1;
  }
  while (idx < src.length) {
    const c = src.charCodeAt(idx);
    if (c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d) { idx++; continue; }
    return idx;
  }
  return -1;
}

// Returns the string at key, or null if the key is missing OR the value
// is not a JSON string.
function readString(src: string, key: string): string | null {
  const start = findValueStart(src, key);
  if (start < 0) return null;
  if (src.charCodeAt(start) != 0x22 /* " */) return null;
  let i = start + 1;
  const out = new Array<i32>();
  while (i < src.length) {
    const c = src.charCodeAt(i);
    if (c == 0x22 /* " */) return String.fromCharCodes(out);
    if (c == 0x5c /* \ */ && i + 1 < src.length) {
      const n = src.charCodeAt(i + 1);
      if (n == 0x22) { out.push(0x22); i += 2; continue; }
      if (n == 0x5c) { out.push(0x5c); i += 2; continue; }
      if (n == 0x2f) { out.push(0x2f); i += 2; continue; }
      if (n == 0x6e) { out.push(0x0a); i += 2; continue; }
      if (n == 0x74) { out.push(0x09); i += 2; continue; }
      if (n == 0x72) { out.push(0x0d); i += 2; continue; }
      i += 2;
      continue;
    }
    out.push(c);
    i++;
  }
  return null;
}

// Sentinel for "missing or non-numeric"; ages 0..150 fit easily inside i32.
const AGE_MISSING: i64 = i64.MIN_VALUE;

function readNumber(src: string, key: string): i64 {
  const start = findValueStart(src, key);
  if (start < 0) return AGE_MISSING;
  let i = start;
  let neg = false;
  if (src.charCodeAt(i) == 0x2d /* - */) { neg = true; i++; }
  let n: i64 = 0;
  let digits = 0;
  while (i < src.length) {
    const c = src.charCodeAt(i);
    if (c >= 0x30 && c <= 0x39) { n = n * 10 + (c - 0x30); digits++; i++; continue; }
    break;
  }
  if (digits == 0) return AGE_MISSING;
  return neg ? -n : n;
}

function trim(s: string): string {
  let lo = 0;
  let hi = s.length;
  while (lo < hi) {
    const c = s.charCodeAt(lo);
    if (c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d) { lo++; continue; }
    break;
  }
  while (hi > lo) {
    const c = s.charCodeAt(hi - 1);
    if (c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d) { hi--; continue; }
    break;
  }
  return s.substring(lo, hi);
}

function toLower(s: string): string {
  // ASCII fold — emails are ASCII case-folded per the JS reference.
  const out = new Array<i32>(s.length);
  for (let i = 0; i < s.length; i++) {
    let c = s.charCodeAt(i);
    if (c >= 0x41 && c <= 0x5a) c += 0x20;
    out[i] = c;
  }
  return String.fromCharCodes(out);
}

function indexOfChar(s: string, ch: i32): i32 {
  for (let i = 0; i < s.length; i++) {
    if (s.charCodeAt(i) == ch) return i;
  }
  return -1;
}

function jsonEscape(s: string): string {
  let out = "";
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c == 0x22) { out += '\\"'; continue; }
    if (c == 0x5c) { out += "\\\\"; continue; }
    if (c == 0x0a) { out += "\\n"; continue; }
    if (c == 0x0d) { out += "\\r"; continue; }
    if (c == 0x09) { out += "\\t"; continue; }
    out += String.fromCharCode(c);
  }
  return out;
}

// --- exported entry point ---------------------------------------------------

export function validate(ptr: i32, len: i32): i64 {
  const src = readUtf8(ptr, len);

  // The JS fixtures reject null / non-object up front. Without a full
  // parser we use the cheapest invariant: the payload's first non-space
  // byte must be '{'.
  let i = 0;
  while (i < src.length) {
    const c = src.charCodeAt(i);
    if (c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d) { i++; continue; }
    break;
  }
  if (i >= src.length || src.charCodeAt(i) != 0x7b /* { */) {
    return pack('{"ok":false,"error":"input must be an object"}');
  }

  const email = readString(src, "email");
  if (email == null || (email as string).length == 0) {
    return pack('{"ok":false,"error":"email required"}');
  }

  const age = readNumber(src, "age");
  if (age == AGE_MISSING || age < 0 || age > 150) {
    return pack('{"ok":false,"error":"age out of range"}');
  }

  const norm = toLower(trim(email as string));
  const at = indexOfChar(norm, 0x40 /* @ */);
  if (at <= 0 || at == norm.length - 1) {
    return pack('{"ok":false,"error":"email shape"}');
  }
  const domain = norm.substring(at + 1);
  if (indexOfChar(domain, 0x2e /* . */) < 0) {
    return pack('{"ok":false,"error":"email domain"}');
  }

  return pack('{"ok":true,"email":"' + jsonEscape(norm) + '","age":' + age.toString() + "}");
}
