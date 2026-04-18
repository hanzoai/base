// Package tasks provides the Hanzo Tasks client for Go applications.
//
// Two methods, two use cases:
//
//	app.Tasks().Add("settlement", "30s", fn)           // recurring (duration)
//	app.Tasks().Add("daily-cleanup", "0 3 * * *", fn)  // recurring (cron)
//	app.Tasks().Now("webhook.deliver", payload)         // fire once immediately
//
// Add() auto-detects duration strings vs cron expressions.
// Transport: ZAP (binary) > HTTP > local goroutine.
package tasks
