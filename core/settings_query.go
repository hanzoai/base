package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hanzoai/base/tools/security"
	"github.com/hanzoai/base/tools/types"
)

type Param struct {
	BaseModel

	Created types.DateTime `db:"created" json:"created"`
	Updated types.DateTime `db:"updated" json:"updated"`
	Value   types.JSONRaw  `db:"value" json:"value"`
}

func (m *Param) TableName() string {
	return paramsTable
}

// ReloadSettings initializes and reloads the stored application settings.
//
// If no settings were stored it will persist the current app ones.
func (app *BaseApp) ReloadSettings() error {
	param := &Param{}
	err := app.ModelQuery(param).Model(paramsKeySettings, param)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// no settings were previously stored -> save
	// (ReloadSettings() will be invoked again by a system hook after successful save)
	if param.Id == "" {
		// force insert in case the param entry was deleted manually after application start
		app.Settings().MarkAsNew()
		return app.Save(app.Settings())
	}

	event := new(SettingsReloadEvent)
	event.App = app

	return app.OnSettingsReload().Trigger(event, func(e *SettingsReloadEvent) error {
		return e.App.Settings().loadParam(e.App, param)
	})
}

// loadParam loads the settings from the stored param into the app ones.
//
// @todo note that the encryption may get removed in the future since it doesn't
// really accomplish much and it might be better to find a way to encrypt the backups
// or implement support for resolving env variables.
func (s *Settings) loadParam(app App, param *Param) error {
	// try first without decryption
	s.mu.Lock()
	plainDecodeErr := json.Unmarshal(param.Value, s)
	s.mu.Unlock()

	// failed, try to decrypt
	if plainDecodeErr != nil {
		encryptionKey := os.Getenv(app.EncryptionEnv())

		// load without decryption has failed and there is no encryption key to use for decrypt
		if encryptionKey == "" {
			return fmt.Errorf("invalid settings db data or missing encryption key %q", app.EncryptionEnv())
		}

		// decrypt
		decrypted, decryptErr := security.Decrypt(string(param.Value), encryptionKey)
		if decryptErr != nil {
			return decryptErr
		}

		// decode again
		s.mu.Lock()
		decryptedDecodeErr := json.Unmarshal(decrypted, s)
		s.mu.Unlock()
		if decryptedDecodeErr != nil {
			return decryptedDecodeErr
		}
	}

	if err := s.PostScan(); err != nil {
		return err
	}
	s.applyStorageEnv()
	return nil
}

// applyStorageEnv lets deployment env drive the S3 blob backend so the single
// selection point (BaseApp.NewFilesystem) stays the one switch — env supplies
// the value the same way DATA_DIR feeds the data dir. No-op unless S3_ENABLED,
// so local-disk dev is unchanged. Mirrors the ecosystem S3_* contract.
func (s *Settings) applyStorageEnv() {
	if strings.ToLower(os.Getenv("S3_ENABLED")) != "true" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.S3.Enabled = true
	if v := os.Getenv("S3_ENDPOINT"); v != "" {
		s.S3.Endpoint = v
	}
	if v := firstEnv("S3_BUCKET_NAME", "S3_BUCKET"); v != "" {
		s.S3.Bucket = v
	}
	if v := os.Getenv("S3_REGION"); v != "" {
		s.S3.Region = v
	}
	if v := os.Getenv("S3_ACCESS_KEY"); v != "" {
		s.S3.AccessKey = v
	}
	if v := os.Getenv("S3_SECRET_KEY"); v != "" {
		s.S3.Secret = v
	}
	if v := os.Getenv("S3_FORCE_PATH_STYLE"); v != "" {
		s.S3.ForcePathStyle, _ = strconv.ParseBool(v)
	}
}

// firstEnv returns the first non-empty environment variable among names.
func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}
