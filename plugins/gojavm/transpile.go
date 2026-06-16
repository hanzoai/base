package gojavm

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/hanzoai/base/plugins/extruntime"
)

// esmRe detects ES-module / CommonJS syntax in a plain .js file so we
// know whether a bundle pass is needed. TypeScript / JSX / .mjs always go
// through esbuild regardless.
var esmRe = regexp.MustCompile(`(?m)^\s*(import|export)\b|require\s*\(|module\.exports|exports\.`)

// transpile reads the module's entry source and returns a CommonJS
// program ready for zip's JSRuntime.LoadModule — i.e. a module whose
// `module.exports` carries the extension's functions, resolvable via
// require(key).
//
// TypeScript, JSX and ES-module sources are bundled with esbuild: types
// stripped, local imports + node_modules folded into one CommonJS module
// targeting ES2015 (goja's level). Plain global-function .js (no module
// syntax) is wrapped so its top-level function declarations become
// exports, preserving the legacy authoring style.
//
// TODO(zip/runtime): zip's runtime.TranspileToES5 uses esbuild's
// single-file Transform (no Bundle, no multi-file import graph). gojavm
// needs api.Build with Bundle:true to fold a whole TS backend directory
// into one program, which zip does not yet expose. When zip grows a
// bundling Transpile (BundleDir / multi-entry) this esbuild call moves
// into zip/runtime and gojavm drops its direct esbuild dependency. Until
// then the bundle step lives here. Tracked on hanzoai/zip PR #9 (issues
// disabled there).
func transpile(dir string, m *extruntime.Manifest) (string, error) {
	src := m.Module
	if src == "" {
		src = "index.js"
	}
	srcPath := filepath.Join(dir, src)
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("gojavm: read %s: %w", srcPath, err)
	}

	ext := strings.ToLower(filepath.Ext(srcPath))
	needsBundle := false
	switch ext {
	case ".ts", ".tsx", ".jsx", ".mts", ".cts", ".mjs":
		needsBundle = true // types / JSX / ESM always need esbuild
	default:
		needsBundle = esmRe.Match(data)
	}

	if !needsBundle {
		// Legacy global-function script: top-level `function fn(){}`
		// declarations are not exports in CommonJS. Re-export every
		// top-level function name so require(key)[fn] resolves, keeping
		// the pre-zip authoring style working unchanged.
		return wrapGlobalFns(string(data)), nil
	}

	result := api.Build(api.BuildOptions{
		EntryPoints: []string{srcPath},
		Bundle:      true,
		Format:      api.FormatCommonJS,
		Platform:    api.PlatformNode,
		Target:      api.ES2015,
		Write:       false,
		LogLevel:    api.LogLevelSilent,
		Sourcemap:   api.SourceMapNone,
		// Node built-ins (fs, crypto, …) stay external; if a migrated
		// backend actually calls one, require() throws at runtime via
		// goja_nodejs rather than silently no-op'ing.
	})
	if len(result.Errors) > 0 {
		return "", fmt.Errorf("gojavm: esbuild %s: %s", srcPath, esbuildErrors(result.Errors))
	}
	if len(result.OutputFiles) == 0 {
		return "", fmt.Errorf("gojavm: esbuild %s: produced no output", srcPath)
	}
	return string(result.OutputFiles[0].Contents), nil
}

// topLevelFnRe finds top-level `function name(` declarations so a legacy
// global-function script can have its functions re-exported on module.exports.
var topLevelFnRe = regexp.MustCompile(`(?m)^\s*function\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)

// wrapGlobalFns appends a CommonJS export footer to a legacy script,
// binding every top-level function declaration onto module.exports. The
// original source still runs verbatim (declarations hoist), so behavior
// is identical to the pre-zip "call globalThis.fn" path — only the
// resolution surface changes from globalThis to module.exports.
func wrapGlobalFns(src string) string {
	matches := topLevelFnRe.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		return src
	}
	var b strings.Builder
	b.WriteString(src)
	b.WriteString("\n;(function(){ if (typeof module === 'undefined' || !module.exports) return;\n")
	seen := map[string]bool{}
	for _, mm := range matches {
		name := mm[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		fmt.Fprintf(&b, "  try { module.exports[%q] = %s; } catch (e) {}\n", name, name)
	}
	b.WriteString("})();\n")
	return b.String()
}

func esbuildErrors(errs []api.Message) string {
	var b bytes.Buffer
	for i, e := range errs {
		if i > 0 {
			b.WriteString("; ")
		}
		if e.Location != nil {
			fmt.Fprintf(&b, "%s:%d: %s", e.Location.File, e.Location.Line, e.Text)
		} else {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}
