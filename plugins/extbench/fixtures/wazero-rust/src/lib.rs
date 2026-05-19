// Pure-Rust validate fixture for the Base extbench harness.
//
// Host-guest ABI (HIP-0105 pointer/length convention, matches
// plugins/wasmvm/module.go):
//
//   __base_alloc(size: i32) -> i32          host -> guest scratch
//   __base_free(ptr: i32, size: i32)        host releases scratch
//   validate(ptr: i32, len: i32) -> i64     packed: (resPtr<<32) | resLen
//
// The host writes a JSON payload at ptr/len, calls validate, then reads
// resLen bytes at resPtr (UTF-8 JSON) and frees them via __base_free.
//
// Shared semantics across all extbench fixtures (canonical reference:
// goja-js/validate.js):
//
//   in:  {"email":"Foo@Example.COM ","age":25}
//   ok:  {"ok":true,"email":"foo@example.com","age":25}
//   err: {"ok":false,"error":"input must be an object"}
//   err: {"ok":false,"error":"email required"}
//   err: {"ok":false,"error":"age out of range"}
//   err: {"ok":false,"error":"email shape"}
//   err: {"ok":false,"error":"email domain"}
//
// We build for wasm32-unknown-unknown (no WASI), so the runtime surface
// is purely linear-memory + exported functions. The Base wasmvm host
// instantiates WASI snapshot_preview1 globally; modules that don't
// import any WASI symbols (like this one) ignore it.

#![no_std]

extern crate alloc;

use alloc::alloc::{alloc, dealloc, Layout};
use alloc::string::{String, ToString};
use core::panic::PanicInfo;

// wasm32 has no default panic handler with #![no_std]. abort by trap.
#[panic_handler]
fn panic(_: &PanicInfo) -> ! {
    core::arch::wasm32::unreachable()
}

// ---------- memory exports --------------------------------------------------

// Allocate `size` bytes and return the raw address. We over-allocate by
// `size_of::<usize>()` to stash the layout's size next to the buffer so
// __base_free can reconstruct the same Layout (alignment is fixed at 8
// to match Rust's default for byte buffers; that's safe for the JSON
// payloads we round-trip).
const ALIGN: usize = 8;

#[no_mangle]
pub extern "C" fn __base_alloc(size: i32) -> i32 {
    if size <= 0 {
        return 0;
    }
    let size = size as usize;
    // We need to remember `size` for the matching dealloc. Pack it as a
    // little-endian u32 in the first 8 bytes (8 so the user pointer stays
    // 8-byte-aligned) and return a pointer past the prefix.
    let total = size + ALIGN;
    let layout = match Layout::from_size_align(total, ALIGN) {
        Ok(l) => l,
        Err(_) => return 0,
    };
    unsafe {
        let raw = alloc(layout);
        if raw.is_null() {
            return 0;
        }
        // Stash size as u32 in first 4 bytes of the prefix; the rest
        // of the prefix is padding to maintain alignment.
        (raw as *mut u32).write(size as u32);
        // User pointer is past the prefix.
        raw.add(ALIGN) as i32
    }
}

#[no_mangle]
pub extern "C" fn __base_free(ptr: i32, _size: i32) {
    if ptr == 0 {
        return;
    }
    unsafe {
        let user = ptr as *mut u8;
        let raw = user.sub(ALIGN);
        let size = (raw as *const u32).read() as usize;
        let total = size + ALIGN;
        if let Ok(layout) = Layout::from_size_align(total, ALIGN) {
            dealloc(raw, layout);
        }
    }
}

// ---------- validate --------------------------------------------------------

#[no_mangle]
pub extern "C" fn validate(ptr: i32, len: i32) -> i64 {
    let input = unsafe { read_input(ptr, len) };
    let json = match input {
        Ok(s) => s,
        Err(_) => return pack(r#"{"ok":false,"error":"input must be an object"}"#),
    };

    // Minimal tolerant JSON scrape. We never need a full parser — the
    // benchmark payload is fixed-shape `{"email":<string>,"age":<int>}`.
    let email = match read_string(&json, "email") {
        Some(s) => s,
        None => return pack(r#"{"ok":false,"error":"email required"}"#),
    };
    if email.is_empty() {
        return pack(r#"{"ok":false,"error":"email required"}"#);
    }
    let age = match read_int(&json, "age") {
        Some(n) => n,
        None => return pack(r#"{"ok":false,"error":"age out of range"}"#),
    };
    if !(0..=150).contains(&age) {
        return pack(r#"{"ok":false,"error":"age out of range"}"#);
    }

    let normalized = email.trim().to_ascii_lowercase();
    let at = match normalized.find('@') {
        Some(i) if i > 0 && i < normalized.len() - 1 => i,
        _ => return pack(r#"{"ok":false,"error":"email shape"}"#),
    };
    let domain = &normalized[at + 1..];
    if !domain.contains('.') {
        return pack(r#"{"ok":false,"error":"email domain"}"#);
    }

    // Manually assemble result JSON. The email is the only user-supplied
    // string and we only allow ascii lowercase + a few separators after
    // normalisation, but escape conservatively anyway.
    let mut out = String::with_capacity(48 + normalized.len());
    out.push_str(r#"{"ok":true,"email":""#);
    json_escape(&mut out, &normalized);
    out.push_str(r#"","age":"#);
    push_i64(&mut out, age);
    out.push('}');
    pack(&out)
}

// ---------- helpers ---------------------------------------------------------

// SAFETY: caller has written `len` bytes at `ptr`. We copy them out into
// a Rust string so we can drop the raw pointer immediately.
unsafe fn read_input(ptr: i32, len: i32) -> Result<String, ()> {
    if ptr == 0 || len < 0 {
        return Err(());
    }
    let bytes = core::slice::from_raw_parts(ptr as *const u8, len as usize);
    core::str::from_utf8(bytes).map(|s| s.to_string()).map_err(|_| ())
}

// pack writes `s` into a freshly-allocated buffer and returns the
// (ptr<<32) | len i64 that the host decodes. The buffer is owned by the
// host after return; the host frees it via __base_free once it's read.
fn pack(s: &str) -> i64 {
    let bytes = s.as_bytes();
    let len = bytes.len();
    let ptr = __base_alloc(len as i32);
    if ptr == 0 {
        return 0;
    }
    unsafe {
        core::ptr::copy_nonoverlapping(bytes.as_ptr(), ptr as *mut u8, len);
    }
    ((ptr as i64) << 32) | (len as i64)
}

// find_value_start locates the byte index of the first non-whitespace
// character after `"<key>":`. Returns None if the key isn't present.
fn find_value_start(src: &str, key: &str) -> Option<usize> {
    // Build the needle on the stack to avoid an allocation for typical
    // short keys. We just want byte search; rust's str::find is fine.
    let mut needle = [0u8; 16];
    let needle_len = 1 + key.len() + 1;
    if needle_len > needle.len() {
        return None;
    }
    needle[0] = b'"';
    needle[1..1 + key.len()].copy_from_slice(key.as_bytes());
    needle[1 + key.len()] = b'"';
    let needle = core::str::from_utf8(&needle[..needle_len]).ok()?;
    let start = src.find(needle)?;
    let mut idx = start + needle_len;
    let bytes = src.as_bytes();
    while idx < bytes.len() {
        match bytes[idx] {
            b':' => {
                idx += 1;
                break;
            }
            b' ' | b'\t' | b'\n' | b'\r' => idx += 1,
            _ => return None,
        }
    }
    while idx < bytes.len() {
        match bytes[idx] {
            b' ' | b'\t' | b'\n' | b'\r' => idx += 1,
            _ => return Some(idx),
        }
    }
    None
}

fn read_string(src: &str, key: &str) -> Option<String> {
    let start = find_value_start(src, key)?;
    let bytes = src.as_bytes();
    if bytes[start] != b'"' {
        return None;
    }
    let mut out = String::new();
    let mut i = start + 1;
    while i < bytes.len() {
        match bytes[i] {
            b'"' => return Some(out),
            b'\\' if i + 1 < bytes.len() => {
                let n = bytes[i + 1];
                match n {
                    b'"' => out.push('"'),
                    b'\\' => out.push('\\'),
                    b'/' => out.push('/'),
                    b'n' => out.push('\n'),
                    b't' => out.push('\t'),
                    b'r' => out.push('\r'),
                    _ => { /* drop unknown escapes — tolerant scrape */ }
                }
                i += 2;
            }
            c => {
                out.push(c as char);
                i += 1;
            }
        }
    }
    None
}

fn read_int(src: &str, key: &str) -> Option<i64> {
    let start = find_value_start(src, key)?;
    let bytes = src.as_bytes();
    let mut i = start;
    let mut neg = false;
    if bytes[i] == b'-' {
        neg = true;
        i += 1;
    }
    if i == bytes.len() || !bytes[i].is_ascii_digit() {
        return None;
    }
    let mut n: i64 = 0;
    while i < bytes.len() && bytes[i].is_ascii_digit() {
        n = n.checked_mul(10)?.checked_add((bytes[i] - b'0') as i64)?;
        i += 1;
    }
    Some(if neg { -n } else { n })
}

fn json_escape(out: &mut String, s: &str) {
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => {
                let mut buf = [0u8; 6];
                let _ = write_unicode_escape(&mut buf, c);
                out.push_str(core::str::from_utf8(&buf).unwrap_or(""));
            }
            c => out.push(c),
        }
    }
}

fn write_unicode_escape(buf: &mut [u8; 6], c: char) -> usize {
    let hex = b"0123456789abcdef";
    let n = c as u32;
    buf[0] = b'\\';
    buf[1] = b'u';
    buf[2] = hex[((n >> 12) & 0xf) as usize];
    buf[3] = hex[((n >> 8) & 0xf) as usize];
    buf[4] = hex[((n >> 4) & 0xf) as usize];
    buf[5] = hex[(n & 0xf) as usize];
    6
}

fn push_i64(out: &mut String, n: i64) {
    if n == 0 {
        out.push('0');
        return;
    }
    let mut buf = [0u8; 20];
    let mut idx = buf.len();
    let mut v = if n < 0 {
        out.push('-');
        (n as i128).unsigned_abs() as u64
    } else {
        n as u64
    };
    while v > 0 {
        idx -= 1;
        buf[idx] = b'0' + (v % 10) as u8;
        v /= 10;
    }
    out.push_str(core::str::from_utf8(&buf[idx..]).unwrap_or("0"));
}

// ---------- allocator -------------------------------------------------------
//
// no_std + dynamic alloc requires choosing an allocator. wee_alloc is
// archived; we use the bump allocator approach via dlmalloc which is
// what wasi-libc ships and what alloc::alloc::Global expects.
//
// For wasm32-unknown-unknown there is no default allocator. Provide
// the minimal one — dlmalloc via the `dlmalloc` crate would pull deps;
// instead we provide a trivial growable bump allocator that's
// adequate for our test workload (one alloc per call, freed immediately).

use core::alloc::{GlobalAlloc, Layout as CoreLayout};
use core::cell::UnsafeCell;
use core::sync::atomic::{AtomicUsize, Ordering};

struct BumpAlloc {
    // current offset into the heap, in bytes. Linear-memory page is 64KB;
    // we grow on demand via core::arch::wasm32::memory_grow.
    offset: AtomicUsize,
    // Free list for layouts <= 256 bytes, indexed by size class.
    // Heap-allocated singly linked list — wasm linear memory is the
    // backing store. Stored inside an UnsafeCell because we can't have
    // a Mutex in no_std and wasm is single-threaded by default.
    free_lists: UnsafeCell<[*mut u8; 33]>, // 8..=256 in 8-byte steps
}

unsafe impl Sync for BumpAlloc {}

const HEAP_BASE: usize = 0x10000; // leave the first page (64 KB) for stack
const PAGE_BYTES: usize = 65536;

unsafe impl GlobalAlloc for BumpAlloc {
    unsafe fn alloc(&self, layout: CoreLayout) -> *mut u8 {
        let size = layout.size().max(8);
        let align = layout.align().max(8);

        // Try the free list for small allocations to keep the bump cursor
        // from running away. The benchmark loop is alloc/free in pairs so
        // recycling is the difference between linear and constant heap.
        if size <= 256 && align <= 8 {
            let idx = (size + 7) / 8 - 1;
            let lists = self.free_lists.get();
            let head = (*lists)[idx];
            if !head.is_null() {
                let next = (head as *mut *mut u8).read();
                (*lists)[idx] = next;
                return head;
            }
        }

        // Bump path. Align up the current cursor to `align`.
        let mut cur = self.offset.load(Ordering::Relaxed);
        if cur == 0 {
            cur = HEAP_BASE;
        }
        let aligned = (cur + align - 1) & !(align - 1);
        let new_end = aligned + size;

        // Grow linear memory if needed.
        let cur_pages = core::arch::wasm32::memory_size(0);
        let need_pages = (new_end + PAGE_BYTES - 1) / PAGE_BYTES;
        if need_pages > cur_pages {
            let extra = need_pages - cur_pages;
            if core::arch::wasm32::memory_grow(0, extra) == usize::MAX {
                return core::ptr::null_mut();
            }
        }

        self.offset.store(new_end, Ordering::Relaxed);
        aligned as *mut u8
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: CoreLayout) {
        let size = layout.size().max(8);
        let align = layout.align().max(8);
        if size <= 256 && align <= 8 {
            let idx = (size + 7) / 8 - 1;
            let lists = self.free_lists.get();
            let head = (*lists)[idx];
            (ptr as *mut *mut u8).write(head);
            (*lists)[idx] = ptr;
        }
        // Large allocations are not recycled — fine for the bench
        // workload (one ~50-byte JSON in/out per call).
    }
}

#[global_allocator]
static ALLOC: BumpAlloc = BumpAlloc {
    offset: AtomicUsize::new(0),
    free_lists: UnsafeCell::new([core::ptr::null_mut(); 33]),
};
