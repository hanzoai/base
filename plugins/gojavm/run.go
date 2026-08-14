package gojavm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/hanzoai/base/tools/security"
)

// The JS backend for functions: source handed over at call time, run on the one
// engine this process has, with the caller's host bound for exactly the length
// of the call.
//
// Registering from an init is what makes a language available — a deployment
// that imports this package can run "js" functions, and one that does not gets a
// clear refusal rather than a silently different behaviour.
func init() { extruntime.Register("js", Run) }

// entry is the one name a function declares. A single entry means the record
// carries source and nothing else: no export list to keep in step with the code,
// and no per-call argument deciding which half of a file runs.
const entry = "handler"

// dispatch is the global the guest reaches its host through. It is one function
// for every capability and every concurrent call, because the pooled VMs are
// shared and a per-call global would be a per-call race.
const dispatch = "__hzhost"

// Run evaluates src as one function call. It satisfies [extruntime.Runner].
//
// A call is admitted only when the engine has a warm VM for it: a pool asked for
// more than it holds builds VMs past it, and every VM the engine is handed stays
// reachable for the life of the process. So the pool size is also the number of
// functions that run at once, and the rest wait here or leave with their ctx's
// error.
func Run(ctx context.Context, src string, payload []byte, host extruntime.Host) ([]byte, error) {
	rt, err := shared()
	if err != nil {
		return nil, err
	}

	select {
	case admit <- struct{}{}:
		defer func() { <-admit }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// The token names this call's host and nothing else. It is live for the
	// length of the call, so source that keeps a copy keeps a name for something
	// already forgotten.
	tok := security.RandomString(32)
	live.put(tok, host)
	defer live.del(tok)

	out, err := rt.EvalContext(ctx, driver(tok, host, payload, src))
	if err != nil {
		return nil, err
	}

	s, ok := out.(string)
	if !ok {
		return []byte("null"), nil
	}
	return []byte(s), nil
}

// driver is the program one call runs: the guest's host object, its payload, its
// own source, and the call of [entry] against the two.
//
// The source is spliced into a function body rather than loaded as a module
// because a module registration is remembered by every VM in the pool for the
// life of the process, and a function is called far more often than it is
// written. Nothing here outlives the call.
func driver(tok string, host extruntime.Host, payload []byte, src string) string {
	var b strings.Builder

	b.WriteString("(function(){\n\"use strict\";\nvar __t=")
	b.WriteString(jsStr(tok))
	b.WriteString(";\nvar __h={};\n")
	for _, name := range host.Names() {
		// Each capability is the same call under a different name, so the guest
		// reads as `base.list({...})` while the runtime still knows none of them.
		fmt.Fprintf(&b, "__h[%s]=function(a){return JSON.parse(%s(__t,%s,JSON.stringify(a===undefined?null:a)));};\n",
			jsStr(name), dispatch, jsStr(name))
	}
	b.WriteString("var __p=")
	b.WriteString(payloadExpr(payload))
	b.WriteString(";\n")
	b.WriteString(src)
	b.WriteString("\n;if(typeof ")
	b.WriteString(entry)
	b.WriteString("!==\"function\"){throw new Error(")
	b.WriteString(jsStr("a function must declare `function " + entry + "(payload, base)`"))
	b.WriteString(");}\nvar __r=")
	b.WriteString(entry)
	b.WriteString("(__p,__h);\nif(__r&&typeof __r.then===\"function\"){throw new Error(")
	b.WriteString(jsStr("a function must answer without waiting"))
	b.WriteString(");}\nreturn __r===undefined?\"null\":JSON.stringify(__r);\n})()")

	return b.String()
}

// jsSeparators are the two code points that may sit unescaped inside a JSON
// string and end a line inside a JS one. Escaping them is what lets a payload be
// written as a literal instead of parsed a second time.
var jsSeparators = strings.NewReplacer(
	string(rune(0x2028)), `\u2028`,
	string(rune(0x2029)), `\u2029`,
)

// payloadExpr renders the payload as the JS expression the guest receives. A
// body that is absent or not JSON is null, which is a value a function can test
// rather than a parse that fails inside it.
func payloadExpr(payload []byte) string {
	if len(payload) == 0 || !json.Valid(payload) {
		return "null"
	}
	return jsSeparators.Replace(string(payload))
}
