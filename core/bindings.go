package core

// AppBindings is what every Base this process opens binds to its own hooks, and
// it is read where a Base is constructed — so a binding registered here reaches
// an org's Base as well as the process's own.
//
// It is the hook counterpart of [AppMigrations]: one list, read in one place, so
// there is a single answer to what a Base does. Anything bound instead at the
// process's front door belongs to the one Base that door opens onto.
//
// Register from an init. A binding added once a Base exists does not reach it.
//
// What goes here runs on every Base, which is the whole point and also the
// limit: it may hold nothing that reaches past the Base it is handed.
var AppBindings BindingsList

// BindingsList is an ordered list of functions applied to a newly constructed App.
type BindingsList struct {
	list []func(App)
}

// Register adds fn to what every Base binds.
func (l *BindingsList) Register(fn func(App)) {
	l.list = append(l.list, fn)
}

// apply runs each registered binding against app, in registration order.
func (l *BindingsList) apply(app App) {
	for _, bind := range l.list {
		bind(app)
	}
}
