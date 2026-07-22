// Package scheduling is the native Base + IAM booking backend — the public
// booking API over the scheduling collections defined in migration
// 1780700000_scheduling.go (eventType, availabilitySchedule, booking,
// bookingAttendee). It turns Base into the backend a booking page (cal.hanzo.ai)
// talks to: read an event type, compute open slots from the host's availability
// minus their existing bookings and synced calendar, create a booking, and let
// an attendee view or cancel it by its opaque uid.
package scheduling

import (
	"os"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/router"
)

// Config controls the plugin. The booking API is opt-in.
type Config struct {
	Enabled bool
}

// ConfigFromEnv reads SCHEDULING_ENABLED.
func ConfigFromEnv() Config {
	return Config{Enabled: os.Getenv("SCHEDULING_ENABLED") == "true"}
}

type plugin struct {
	app    core.App
	config Config
}

// MustRegister registers the scheduling plugin and panics on error.
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register mounts the public booking API on the app when enabled.
func Register(app core.App, config Config) error {
	if !config.Enabled {
		return nil
	}
	p := &plugin{app: app, config: config}
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id: "scheduling",
		Func: func(e *core.ServeEvent) error {
			p.registerRoutes(e.Router)
			return e.Next()
		},
	})
	return nil
}

// registerRoutes mounts the booking API under /v1. Event-type and slot reads plus
// booking creation are public (a booking page is public); a booking is then
// managed by its opaque uid, so those routes need no auth either.
func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	g := r.Group("/v1/schedule")
	g.GET("/{owner}/{slug}", p.handleGetEventType)
	g.GET("/{owner}/{slug}/slots", p.handleGetSlots)
	g.POST("/{owner}/{slug}/book", p.handleBook)

	b := r.Group("/v1/booking")
	b.GET("/{uid}", p.handleGetBooking)
	b.POST("/{uid}/cancel", p.handleCancelBooking)
}
