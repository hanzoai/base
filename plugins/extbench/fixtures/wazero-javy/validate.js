// Javy fixture — Shopify's Javy compiles standard JS (QuickJS as wasm) so
// modern ES is fine. Javy's calling convention is stdin -> JSON in,
// stdout -> JSON out (the host writes the payload to fd 0 and reads the
// result from fd 1). That's why this file doesn't export a `validate`
// symbol the way the goja/v8 fixtures do — the entire script *is* the
// invocation.
//
// Shared semantics (canonical: goja-js/validate.js):
//   in:  {"email":"Foo@Example.COM ","age":25}
//   ok:  {"ok":true,"email":"foo@example.com","age":25}
//   err: {"ok":false,"error":"input must be an object"}
//   err: {"ok":false,"error":"email required"}
//   err: {"ok":false,"error":"age out of range"}
//   err: {"ok":false,"error":"email shape"}
//   err: {"ok":false,"error":"email domain"}

function validate(input) {
  if (input === null || typeof input !== "object") {
    return { ok: false, error: "input must be an object" };
  }
  const email = input.email;
  const age = input.age;
  if (typeof email !== "string" || email.length === 0) {
    return { ok: false, error: "email required" };
  }
  if (typeof age !== "number" || age < 0 || age > 150) {
    return { ok: false, error: "age out of range" };
  }
  const normalized = email.trim().toLowerCase();
  const at = normalized.indexOf("@");
  if (at <= 0 || at === normalized.length - 1) {
    return { ok: false, error: "email shape" };
  }
  const domain = normalized.substring(at + 1);
  if (domain.indexOf(".") < 0) {
    return { ok: false, error: "email domain" };
  }
  return { ok: true, email: normalized, age };
}

// Javy stdio bridge.
const Javy = globalThis.Javy;
const input = JSON.parse(Javy.IO.readSync(0));
Javy.IO.writeSync(1, new TextEncoder().encode(JSON.stringify(validate(input))));
