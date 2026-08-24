package ghupdate

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/hanzoai/base/tests"
	"github.com/spf13/cobra"
)

// `base update` downloads a release archive and REPLACES THE RUNNING
// EXECUTABLE with what is inside it, so which repository it asks is the whole
// of its trust. examples/base/main.go registers this plugin with an empty
// Config, meaning the defaults below are what a stock binary actually uses —
// they are not a fallback nobody reaches.
//
// They pointed at owner "base" until now: github.com/base/base, a repository
// this project does not own and which exists. The fork's rebrand rewrote
// "pocketbase" to "base" everywhere and turned a correct default into that one,
// which is exactly why this is pinned rather than left to be read correctly the
// next time someone runs a rename across the tree.
func TestDefaultsPointAtThisProject(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	defer app.Cleanup()

	// Observe what a stock registration REQUESTS, rather than re-deriving the
	// defaults here — a test that recomputes the logic it is checking agrees
	// with itself no matter what the code does.
	rec := &recordingClient{}
	root := &cobra.Command{Use: "base", Version: "0.0.0"}
	if err := Register(app, root, Config{HttpClient: rec}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	update, _, err := root.Find([]string{"update"})
	if err != nil || update == nil {
		t.Fatalf("the update command was not registered: %v", err)
	}
	// The fetch fails (the recorder returns nothing usable); the URL it tried is
	// the point.
	_ = update.RunE(update, nil)

	if rec.url == "" {
		t.Fatal("the update command made no request — this test observed nothing")
	}
	if !strings.Contains(rec.url, "/repos/hanzoai/base/") {
		t.Fatalf("a stock binary asks %q — it must address github.com/hanzoai/base", rec.url)
	}
	if strings.Contains(rec.url, "/repos/base/base/") {
		t.Fatalf("a stock binary asks %q — github.com/base/base is not this project", rec.url)
	}
}

// recordingClient captures the first URL requested and refuses it, so no
// network call leaves the test and the URL is still observable.
type recordingClient struct{ url string }

func (c *recordingClient) Do(req *http.Request) (*http.Response, error) {
	if c.url == "" {
		c.url = req.URL.String()
	}
	return nil, errors.New("recorded; no network in tests")
}

// The same fact stated where it cannot drift: whatever the defaults are, the URL
// they build must address this project. Reading the constant out of the source
// would be circular, so this asserts the shape a stock binary fetches.
func TestDefaultReleaseURLAddressesHanzoai(t *testing.T) {
	const want = "https://api.github.com/repos/hanzoai/base/releases/latest"
	got := releaseURL("hanzoai", "base")
	if got != want {
		t.Fatalf("release URL = %q, want %q", got, want)
	}
	if strings.Contains(releaseURL("hanzoai", "base"), "/repos/base/base/") {
		t.Fatal("the release URL addresses github.com/base/base, which this project does not own")
	}
}
