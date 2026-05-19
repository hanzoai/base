package extruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadManifest reads extension.json from dir and returns the parsed manifest.
// JSON over TOML — base has zero TOML deps and the manifest is small enough
// that the JSON ergonomics tax is irrelevant.
func LoadManifest(dir string) (*Manifest, error) {
	p := filepath.Join(dir, "extension.json")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNoManifest, p)
		}
		return nil, fmt.Errorf("read manifest %s: %w", p, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrBadManifest, p, err)
	}

	if m.Name == "" {
		return nil, fmt.Errorf("%w: %s: name is required", ErrBadManifest, p)
	}
	if m.Runtime == "" {
		return nil, fmt.Errorf("%w: %s: runtime is required (native|goja|wazero|v8go)", ErrBadManifest, p)
	}
	return &m, nil
}
