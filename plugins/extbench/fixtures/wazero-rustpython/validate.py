"""validate(email, age) -- RustPython variant, same semantics.

RustPython implements Python 3.12-ish in Rust. It compiles cleanly to
wasm32-wasi via `cargo build --release --target wasm32-wasi
--features freeze-stdlib`. The freeze-stdlib feature embeds the
standard library into the wasm artifact so `import json` works at
runtime with zero filesystem deps -- important inside wazero where
the guest has no host fs by default.
"""

import json
import sys


def validate(payload):
    if not isinstance(payload, dict):
        return {"ok": False, "error": "input must be an object"}
    email = payload.get("email")
    age = payload.get("age")
    if not isinstance(email, str) or len(email) == 0:
        return {"ok": False, "error": "email required"}
    if not isinstance(age, int) or age < 0 or age > 150:
        return {"ok": False, "error": "age out of range"}
    normalized = email.strip().lower()
    at = normalized.find("@")
    if at <= 0 or at == len(normalized) - 1:
        return {"ok": False, "error": "email shape"}
    domain = normalized[at + 1 :]
    if domain.find(".") < 0:
        return {"ok": False, "error": "email domain"}
    return {"ok": True, "email": normalized, "age": age}


if __name__ == "__main__":
    raw = sys.stdin.read()
    payload = json.loads(raw)
    result = validate(payload)
    sys.stdout.write(json.dumps(result))
