# validate(payload) — same spec as the other validate fixtures.
# Payload: {"email": str, "age": number}
# Result : {"ok": bool, "email"?: str, "age"?: int, "error"?: str}
#
# Pure stdlib only — we're benchmarking the runtime, not the ecosystem.


def validate(p):
    if not isinstance(p, dict):
        return {"ok": False, "error": "input must be an object"}
    email = p.get("email")
    age = p.get("age")
    if not isinstance(email, str) or not email:
        return {"ok": False, "error": "email required"}
    if not isinstance(age, (int, float)) or age < 0 or age > 150:
        return {"ok": False, "error": "age out of range"}
    normalized = email.strip().lower()
    at = normalized.find("@")
    if at <= 0 or at == len(normalized) - 1:
        return {"ok": False, "error": "email shape"}
    domain = normalized[at + 1:]
    if "." not in domain:
        return {"ok": False, "error": "email domain"}
    return {"ok": True, "email": normalized, "age": age}
