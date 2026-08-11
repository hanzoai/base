package core

// NotifyDebounce lets the external test package assert against the same window
// the watcher actually uses, instead of restating 50ms and drifting from it.
// This file is a _test.go, so it widens no public API.
const NotifyDebounce = notifyDebounce
