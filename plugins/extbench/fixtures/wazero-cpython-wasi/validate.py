"""validate(email, age) -- normalize email + check shape.

Same semantics as the goja / wazero-as / pyvm / starlark / native fixtures.

In a WASI-command-module deployment of CPython, this script reads JSON
from stdin and writes JSON to stdout. The host shim (see README) wraps
that into our pointer-based ABI.
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
