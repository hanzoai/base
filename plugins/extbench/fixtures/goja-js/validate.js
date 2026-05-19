// validate(payload) — normalize email + check shape.
// Payload shape: { email: string, age: number }
// Result shape: { ok: bool, email?: string, age?: number, error?: string }
//
// Goja exposes this as globalThis.validate after the script runs once.
function validate(input) {
  if (input === null || typeof input !== "object") {
    return { ok: false, error: "input must be an object" };
  }
  var email = input.email;
  var age = input.age;
  if (typeof email !== "string" || email.length === 0) {
    return { ok: false, error: "email required" };
  }
  if (typeof age !== "number" || age < 0 || age > 150) {
    return { ok: false, error: "age out of range" };
  }
  var normalized = email.trim().toLowerCase();
  // Basic shape check: one '@' with non-empty local + domain containing a dot.
  var at = normalized.indexOf("@");
  if (at <= 0 || at === normalized.length - 1) {
    return { ok: false, error: "email shape" };
  }
  var domain = normalized.substring(at + 1);
  if (domain.indexOf(".") < 0) {
    return { ok: false, error: "email domain" };
  }
  return { ok: true, email: normalized, age: age };
}
