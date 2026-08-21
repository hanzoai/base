package core

import (
	"os"
	"path/filepath"
)

// The two databases a Base keeps in its data directory while the engine keeps
// them in files. "auxiliary" rather than "aux" because the latter is a reserved
// Windows filename (hanzoai/base#5607).
const (
	dataFile = "data.db"
	auxFile  = "auxiliary.db"
)

// Database is what a Base can measure about where it keeps records. Engine
// differences are the dialect's, so both engines answer the same questions and
// the answer has one shape.
//
// Path and size are the app's to report only while the engine keeps the
// database in the data directory. A server keeps it in the server, which is
// also where it is sized, so on one of those they read empty rather than
// naming a file that isn't there.
type Database struct {
	Engine      string               `json:"engine"`
	Local       bool                 `json:"local"`
	Data        DatabaseFile         `json:"data"`
	Aux         DatabaseFile         `json:"aux"`
	Collections []DatabaseCollection `json:"collections"`
}

// DatabaseFile is one database and what it occupies.
type DatabaseFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// DatabaseCollection is one collection and how many records are in it.
//
// Records is null for a collection the engine refused to count — a view over a
// table that no longer exists is the usual reason — so that one of those reads
// as unknown rather than as empty.
type DatabaseCollection struct {
	Records *int64 `json:"records"`
	Id      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	System  bool   `json:"system"`
}

// DescribeDatabase measures where the app keeps its records and how much is
// there. A collection that cannot be counted is reported as uncounted and
// logged, so one broken view leaves the rest of the answer standing.
func DescribeDatabase(app App) (*Database, error) {
	collections, err := app.FindAllCollections()
	if err != nil {
		return nil, err
	}

	db := &Database{
		Engine:      app.Dialect().Name(),
		Local:       LocalDatabase(app.Dialect()),
		Collections: make([]DatabaseCollection, 0, len(collections)),
	}

	if db.Local {
		db.Data = databaseFile(app, dataFile)
		db.Aux = databaseFile(app, auxFile)
	}

	for _, c := range collections {
		entry := DatabaseCollection{Id: c.Id, Name: c.Name, Type: c.Type, System: c.System}

		total, countErr := app.CountRecords(c)
		if countErr != nil {
			app.Logger().Warn("Failed to count a collection's records", "collection", c.Name, "error", countErr.Error())
		} else {
			entry.Records = &total
		}

		db.Collections = append(db.Collections, entry)
	}

	return db, nil
}

// ReclaimDatabase rewrites the database without the pages that deleted rows
// left behind, folds the write-ahead log back into it, and refreshes the
// planner's statistics. It returns what the app kept on disk either side of
// that, so a caller reports what was freed rather than claiming something was.
//
// The rewrite lands through the log like any other write, so the fold comes
// after it — the other order leaves the whole rewrite sitting in the log and
// the app holding more than it started with, which is measured in
// TestDatabaseReclaim. The dialect spells each step for the connected engine;
// an engine with no log to fold spells that one empty and it is skipped.
func ReclaimDatabase(app App) (before, after int64, err error) {
	before = databaseOnDisk(app)

	if err = app.Vacuum(); err != nil {
		return before, databaseOnDisk(app), err
	}

	if err = app.AuxVacuum(); err != nil {
		return before, databaseOnDisk(app), err
	}

	if checkpoint := app.Dialect().Checkpoint(); checkpoint != "" {
		if _, err = app.NonconcurrentDB().NewQuery(checkpoint).Execute(); err != nil {
			return before, databaseOnDisk(app), err
		}
		if _, err = app.AuxNonconcurrentDB().NewQuery(checkpoint).Execute(); err != nil {
			return before, databaseOnDisk(app), err
		}
	}

	if _, err = app.NonconcurrentDB().NewQuery(app.Dialect().Optimize()).Execute(); err != nil {
		return before, databaseOnDisk(app), err
	}

	return before, databaseOnDisk(app), nil
}

// databaseOnDisk is what the app keeps in its data directory, and 0 on a server
// engine, which keeps nothing there. Unexported because a caller that wants it
// has a Database, whose two files carry the same number.
func databaseOnDisk(app App) int64 {
	if !LocalDatabase(app.Dialect()) {
		return 0
	}

	return databaseFile(app, dataFile).Size + databaseFile(app, auxFile).Size
}

// databaseFile sizes one database, counting the write-ahead log and its index
// alongside it — a database that has just been written to keeps most of its
// recent bytes there, so the file on its own reads smaller than the database is.
func databaseFile(app App, name string) DatabaseFile {
	file := DatabaseFile{Path: filepath.Join(app.DataDir(), name)}

	for _, path := range []string{file.Path, file.Path + "-wal", file.Path + "-shm"} {
		if stat, statErr := os.Stat(path); statErr == nil {
			file.Size += stat.Size()
		}
	}

	return file
}
