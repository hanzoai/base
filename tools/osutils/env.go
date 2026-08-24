package osutils

import (
	"os"
	"strconv"
	"strings"
)

// Bool reads name from the environment as a boolean, returning def when it is
// unset, empty, or not something a boolean can be read from.
//
// It accepts what strconv.ParseBool accepts — 1/t/T/TRUE/true/True and their
// false counterparts — which is the answer the rest of this tree already gives
// (tools/filesystem, tools/search, core/settings_query all parse booleans that
// way). The hand-rolled `== "true"` and `!= "1"` checks were the deviation, and
// each turned a reasonable spelling into silence: BOOTNODE_ENABLED=1 left
// bootnode off, ZAP_DISABLED=1 left the listener up, and neither said so. An
// operator's off-switch that does nothing is worse than one that does not exist.
//
// An unparseable value yields def rather than its opposite: someone who wrote
// something meaningless has not asked for the other thing.
func Bool(name string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}
