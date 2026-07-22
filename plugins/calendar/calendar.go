// Package calendar is the native Base + IAM booking backend that speaks Cal.com's
// API-v2 shapes, so a public booking page rendered with Cal's <Booker> atom talks
// straight to Base. It mounts under /v1/calendar and serves the Booker's endpoint
// subset — public event, available slots, slot holds, booking create/read/cancel,
// and an anonymous /me — over the scheduling collections defined in migration
// 1780700000_scheduling.go (eventType, availabilitySchedule, booking). The
// availability computation, the transactional double-book guard and the rate
// limiter are the reviewed core, preserved verbatim; this package only reshapes
// the wire contract from the bespoke /v1/schedule shapes to the Cal atom's.
package calendar

import (
	"os"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/router"
)

// Config controls the plugin. The booking API is opt-in.
type Config struct {
	Enabled bool
}

// ConfigFromEnv reads CALENDAR_ENABLED.
func ConfigFromEnv() Config {
	return Config{Enabled: os.Getenv("CALENDAR_ENABLED") == "true"}
}

type plugin struct {
	app             core.App
	config          Config
	ipLimit         *limiter // booking, per client IP
	hostLimit       *limiter // booking, per host (owner)
	readIPLimit     *limiter // public reads/reserve, per client IP
	readHandleLimit *limiter // public reads, per host handle (hard bound)
	holds           *holds   // advisory short-TTL slot reservations
}

// MustRegister registers the calendar plugin and panics on error.
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
	p := &plugin{
		app:             app,
		config:          config,
		ipLimit:         newLimiter(time.Minute, 15),  // 15 booking attempts/min per client IP
		hostLimit:       newLimiter(time.Minute, 60),  // 60 booking attempts/min per host
		readIPLimit:     newLimiter(time.Minute, 240), // 240 reads/min per client IP (defense-in-depth)
		readHandleLimit: newLimiter(time.Minute, 600), // 600 reads/min per host handle (hard bound)
		holds:           newHolds(5 * time.Minute),    // advisory slot holds expire after 5 minutes
	}
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id: "calendar",
		Func: func(e *core.ServeEvent) error {
			p.registerRoutes(e.Router)
			return e.Next()
		},
	})
	return nil
}

// registerRoutes mounts the Cal-shaped booking API under /v1/calendar. Every route
// is public — a booking page is unauthenticated — so the reads and the
// availability-gated booking write need no auth; a booking is then managed by its
// opaque uid. Each route notes the Cal atom hook it serves.
func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	g := r.Group("/v1/calendar")
	g.GET("/atoms/event-types/{slug}/public", p.handlePublicEvent)    // useAtomGetPublicEvent
	g.GET("/slots/available", p.handleAvailableSlots)                 // useAvailableSlots
	g.POST("/slots/reserve", p.handleReserveSlot)                     // useReserveSlot
	g.DELETE("/slots/selected-slot", p.handleDeleteSelectedSlot)      // useDeleteSelectedSlot
	g.GET("/bookings/{uid}/reschedule", p.handleBookingForReschedule) // useGetBookingForReschedule
	g.POST("/bookings", p.handleCreateBooking)                        // useCreateBooking
	g.GET("/bookings/{uid}", p.handleGetBooking)                      // SPA confirmation view
	g.POST("/bookings/{uid}/cancel", p.handleCancelBooking)           // SPA cancel action
	g.GET("/me", p.handleMe)                                          // useMe (anonymous, non-leaking)
}
