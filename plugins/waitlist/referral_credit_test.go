// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import "testing"

// A referral is COUNTED whatever it is worth. The count and the points are two
// facts: an operator may run POINTS_REFERRAL=0 to track who invited whom while
// paying nothing for it, and the count must still move.
//
// It did not. creditReferrer incremented referralCount in memory and left the
// save to award(), which skips it for a zero-point award — so the increment was
// discarded while the joiner's own referredBy was written, leaving the two
// sides of one referral disagreeing.
func TestReferralIsCountedEvenWhenItPaysNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		pts  int
	}{
		{"paid", 10},
		{"unpaid", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Enabled: true, DefaultSlugs: []string{"launch"}, JoinRateLimit: -1}
			cfg.Points.Referral = tc.pts
			srv := newWaitlistServerWith(t, cfg)

			a := join(t, srv, "alice@example.com", "")
			join(t, srv, "bob@example.com", a.RefCode)

			got := status(t, srv, "alice@example.com")
			if got.ReferralCount != 1 {
				t.Fatalf("referralCount = %d, want 1 (referral worth %d points)", got.ReferralCount, tc.pts)
			}
			if got.Points != tc.pts {
				t.Fatalf("points = %d, want %d", got.Points, tc.pts)
			}

			// The joiner's side must agree: it records who referred it.
			b := status(t, srv, "bob@example.com")
			if b.ReferralCount != 0 {
				t.Fatalf("joiner referralCount = %d, want 0", b.ReferralCount)
			}
		})
	}
}
