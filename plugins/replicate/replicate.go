// Package replicate adds automatic SQLite WAL replication to Base apps.
// One import, zero config files, zero sidecars.
//
// Just set REPLICATE_S3_ENDPOINT and the plugin handles everything.
package replicate

import (
	"os"
	"path/filepath"

	"github.com/hanzoai/base/core"
	rep "github.com/hanzoai/replicate"
)

// MustRegister registers auto-replication for all SQLite DBs in Base's data dir.
// No-op if REPLICATE_S3_ENDPOINT is not set.
func MustRegister(app core.App) {
	if os.Getenv("REPLICATE_S3_ENDPOINT") == "" {
		return
	}

	// Set path prefix to app name if not already set
	if os.Getenv("REPLICATE_S3_PATH") == "" {
		hostname, _ := os.Hostname()
		os.Setenv("REPLICATE_S3_PATH", app.Settings().Meta.AppName+"/"+hostname)
	}

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// Replicate all .db files in the data directory
		filepath.Walk(app.DataDir(), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Ext(path) != ".db" {
				return nil
			}
			rep.AutoReplicate(path)
			return nil
		})
		return e.Next()
	})
}
