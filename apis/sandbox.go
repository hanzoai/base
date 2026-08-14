package apis

import "context"

// Where a function's work runs.
//
// A function starts a sandbox; it never becomes one. Starting hands back the
// name the work is known by, and that is the whole of what crosses back: the
// network, the filesystem and the processes a sandbox has belong to the code
// running there, and a function that started one is still a function with two
// reads and no wire. So this is bound because work costs machines, not because
// it widens what a function can see — those are different questions and only
// the first one is answered here.
//
// The result arrives the way any other writer's does. The work authenticates to
// Base and writes a record; nobody holds a request open for the minutes it
// takes, and the caller finds what it wrote under the name it was given.

// Sandbox is what to run — an image, and the environment it runs under.
//
// It says nothing about WHOSE work it is. That is [Sandboxes.Start]'s org, taken
// from the credential the request already resolved, which is what makes it the
// one thing about a run that the asking cannot state.
type Sandbox struct {
	// Snapshot names the image to run. The work is that image's own entry
	// point, so a run is described by what it starts as and what it starts
	// with, and there is no command here for a caller to compose.
	Snapshot string `json:"snapshot"`

	// Env is what the image reads its instructions from.
	Env map[string]string `json:"env,omitempty"`

	// Labels is the caller's own bookkeeping, carried through untouched.
	Labels map[string]string `json:"labels,omitempty"`
}

// Sandboxes is somewhere isolated to run work, as a Base reaches it.
//
// A deployment installs one under [StoreKeySandboxes]. A deployment that
// installs none still answers the name — it refuses in as many words rather
// than leaving it undefined, so what a function may ask for reads the same
// everywhere and a deployment without a runtime is a clear answer instead of a
// different language.
type Sandboxes interface {
	// Start begins one sandbox for org and answers with the name it is known
	// by. It returns once the work has begun and never waits for it to finish.
	//
	// It must return when ctx is done. The call is made from the function's own
	// slot on a pool the whole process shares, so a Start that outlives its
	// context holds that slot past the deadline the function was given, and the
	// next function waits behind it.
	//
	// org is whose work this is. An implementation carries it to the runtime
	// itself and never reads it back out of s, because s is what a function
	// asked for and org is what the request already proved.
	//
	// It is empty on a Base that serves one tenant, which is every deployment
	// that registers no [Bases] — there is no org because there is nobody to
	// tell apart. An implementation serving many decides what an empty one
	// means to it, and refusing is a reasonable thing to decide.
	Start(ctx context.Context, org string, s Sandbox) (string, error)
}
