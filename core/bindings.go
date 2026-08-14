package core

import "sync"

// AppBindings is what every Base this process opens binds to its own hooks, and
// it is read where a Base is constructed — so a binding registered here reaches
// an org's Base as well as the process's own.
//
// It is the hook counterpart of [AppMigrations]: one list, read in one place, so
// there is a single answer to what a Base does. Anything bound instead at the
// process's front door belongs to the one Base that door opens onto.
//
// Register before the Base that should carry it is constructed. The process's
// own Base is built at startup, so a plugin that registers after that says the
// same thing twice: once on the app it was handed, and once here for every Base
// opened later.
//
// What goes here runs on every Base, which is the whole point and also the
// limit: it may hold nothing that reaches past the Base it is handed.
var AppBindings BindingsList

// BindingsList is an ordered list of functions applied to a newly constructed
// App.
//
// A plugin holds one of its own to collect what it has to say before deciding
// which Bases hear it: Register records a statement, Apply makes it on a Base.
type BindingsList struct {
	mu   sync.RWMutex
	list []func(App)
}

// Register adds fn to what every Base binds.
func (l *BindingsList) Register(fn func(App)) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.list = append(l.list, fn)
}

// Apply runs each registered binding against app, in registration order.
func (l *BindingsList) Apply(app App) {
	l.mu.RLock()
	list := l.list
	l.mu.RUnlock()

	for _, bind := range list {
		bind(app)
	}
}
