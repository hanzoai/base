// Package tasks provides the Hanzo Tasks client for Go applications.
//
// Two methods, two use cases:
//
//	client := tasks.New(os.Getenv("TASKS_URL"), os.Getenv("TASKS_ZAP"), nil)
//	client.Add("settlement.process", "30s", fn)   // recurring schedule
//	client.Now("webhook.deliver", payload)          // fire once immediately
//
// Transport priority: ZAP (binary, low-latency) > HTTP > local goroutine.
// When TASKS_ZAP is set, tasks submit over ZAP binary protocol.
// When TASKS_URL is set, tasks submit over HTTP as fallback.
// When neither is set, tasks run locally via goroutine timers (dev mode).
//
// Integration with Hanzo Base:
//
//	app.Tasks().Add("cleanup", "1h", fn)
//	app.Tasks().Now("email.send", payload)
package tasks
