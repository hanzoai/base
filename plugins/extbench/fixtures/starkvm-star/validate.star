# validate(payload) -- normalize email + check shape.
# Payload shape: {"email": str, "age": int}
# Result shape: {"ok": True, "email": str, "age": int} on success
#                {"ok": False, "error": str} on failure.
#
# Written in the permissive Starlark dialect (FileOptions: While=True,
# TopLevelControl=True, GlobalReassign=True, Recursion=True) to keep
# the implementation byte-equivalent in semantics to the goja/wazero/
# pyvm/native fixtures. Strict Bazel Starlark would forbid `while`
# and require list-comprehensions for the iteration, but the host
# isn't running this under Bazel's security model -- we're running it
# inside an extruntime Module just like every other guest language,
# so the dialect choice is ours.

def validate(input):
    if input == None or type(input) != "dict":
        return {"ok": False, "error": "input must be an object"}
    email = input.get("email")
    age = input.get("age")
    if type(email) != "string" or len(email) == 0:
        return {"ok": False, "error": "email required"}
    if type(age) != "int" or age < 0 or age > 150:
        return {"ok": False, "error": "age out of range"}
    # Strip + lowercase. Starlark strings are immutable so we build a
    # new one by direct method calls; no in-place mutation needed.
    normalized = email.strip().lower()
    # Basic shape check: one '@', non-empty local, domain contains '.'.
    at = normalized.find("@")
    if at <= 0 or at == len(normalized) - 1:
        return {"ok": False, "error": "email shape"}
    domain = normalized[at + 1:]
    if domain.find(".") < 0:
        return {"ok": False, "error": "email domain"}
    return {"ok": True, "email": normalized, "age": age}
