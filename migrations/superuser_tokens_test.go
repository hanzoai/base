package migrations_test

import (
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// TestSuperuserTokensFromBeforeTheRotationStopVerifying pins what an upgrade
// does to the superuser tokens a Base signed itself.
//
// Superusers are IAM's admin org and IAM issues their tokens, so a superuser
// token a Base signed came from sign-in that no longer exists. The rotation
// replaces the secrets such tokens verify against, so each of them stops
// verifying on the first start. A users token is left alone.
func TestSuperuserTokensFromBeforeTheRotationStopVerifying(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	superuser, err := app.FindRecordById(core.CollectionNameSuperusers, "sywbhecnh46rhm0")
	if err != nil {
		t.Fatal(err)
	}
	superuserToken, err := superuser.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}

	user, err := app.FindRecordById("users", "4q1xlclmfloku33")
	if err != nil {
		t.Fatal(err)
	}
	userToken, err := user.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}

	// The test data records the rotation as applied, which keeps its own tokens
	// good. Applying it again is what an upgrade from before it does.
	rerun(t, app, "1789100100_rotate_superuser_tokens.go")

	if _, err := app.FindAuthRecordByToken(superuserToken, core.TokenTypeAuth); err == nil {
		t.Fatal("a superuser token signed before the rotation still verifies")
	}
	if _, err := app.FindAuthRecordByToken(userToken, core.TokenTypeAuth); err != nil {
		t.Fatalf("the rotation stopped a users token verifying: %v", err)
	}
}
