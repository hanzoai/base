package apis

import (
	"encoding/json"
	"sync"
	"time"
)

// PresenceEntry represents a single client's presence in a channel.
type PresenceEntry struct {
	ClientID  string         `json:"clientId"`
	State     map[string]any `json:"state"`
	JoinedAt  time.Time      `json:"joinedAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// PresenceChangeType identifies a presence event kind.
type PresenceChangeType string

const (
	PresenceJoin   PresenceChangeType = "join"
	PresenceLeave  PresenceChangeType = "leave"
	PresenceUpdate PresenceChangeType = "update"
)

// PresenceChange is emitted when presence state changes.
type PresenceChange struct {
	Type     PresenceChangeType `json:"type"`
	Channel  string             `json:"channel"`
	ClientID string             `json:"clientId"`
	State    map[string]any     `json:"state,omitempty"`
}

// PresenceCallback is invoked on presence changes.
type PresenceCallback func(change PresenceChange)

// presenceChannel holds the state for a single presence channel.
type presenceChannel struct {
	mu        sync.RWMutex
	entries   map[string]*PresenceEntry // clientId -> entry
	callbacks []PresenceCallback
}

// PresenceManager tracks online presence across channels.
// Internally uses a map with per-channel mutex for conflict-free tracking.
type PresenceManager struct {
	mu       sync.RWMutex
	channels map[string]*presenceChannel
}

// NewPresenceManager creates a new PresenceManager.
func NewPresenceManager() *PresenceManager {
	return &PresenceManager{
		channels: make(map[string]*presenceChannel),
	}
}

// getOrCreateChannel returns an existing channel or creates one.
func (pm *PresenceManager) getOrCreateChannel(channel string) *presenceChannel {
	pm.mu.RLock()
	ch, ok := pm.channels[channel]
	pm.mu.RUnlock()
	if ok {
		return ch
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()
	// double-check after acquiring write lock
	if ch, ok = pm.channels[channel]; ok {
		return ch
	}
	ch = &presenceChannel{
		entries: make(map[string]*PresenceEntry),
	}
	pm.channels[channel] = ch
	return ch
}

// Join registers a client in a channel with initial state.
func (pm *PresenceManager) Join(channel, clientID string, state map[string]any) {
	ch := pm.getOrCreateChannel(channel)

	now := time.Now()
	ch.mu.Lock()
	ch.entries[clientID] = &PresenceEntry{
		ClientID:  clientID,
		State:     state,
		JoinedAt:  now,
		UpdatedAt: now,
	}
	callbacks := make([]PresenceCallback, len(ch.callbacks))
	copy(callbacks, ch.callbacks)
	ch.mu.Unlock()

	change := PresenceChange{
		Type:     PresenceJoin,
		Channel:  channel,
		ClientID: clientID,
		State:    state,
	}
	for _, cb := range callbacks {
		cb(change)
	}
}

// Leave removes a client from a channel.
func (pm *PresenceManager) Leave(channel, clientID string) {
	pm.mu.RLock()
	ch, ok := pm.channels[channel]
	pm.mu.RUnlock()
	if !ok {
		return
	}

	ch.mu.Lock()
	_, existed := ch.entries[clientID]
	delete(ch.entries, clientID)
	callbacks := make([]PresenceCallback, len(ch.callbacks))
	copy(callbacks, ch.callbacks)
	ch.mu.Unlock()

	if !existed {
		return
	}

	change := PresenceChange{
		Type:     PresenceLeave,
		Channel:  channel,
		ClientID: clientID,
	}
	for _, cb := range callbacks {
		cb(change)
	}
}

// LeaveAll removes a client from all channels.
func (pm *PresenceManager) LeaveAll(clientID string) {
	pm.mu.RLock()
	channels := make([]string, 0, len(pm.channels))
	for name := range pm.channels {
		channels = append(channels, name)
	}
	pm.mu.RUnlock()

	for _, ch := range channels {
		pm.Leave(ch, clientID)
	}
}

// Update updates a client's state in a channel.
func (pm *PresenceManager) Update(channel, clientID string, state map[string]any) {
	pm.mu.RLock()
	ch, ok := pm.channels[channel]
	pm.mu.RUnlock()
	if !ok {
		return
	}

	ch.mu.Lock()
	entry, exists := ch.entries[clientID]
	if exists {
		entry.State = state
		entry.UpdatedAt = time.Now()
	}
	callbacks := make([]PresenceCallback, len(ch.callbacks))
	copy(callbacks, ch.callbacks)
	ch.mu.Unlock()

	if !exists {
		return
	}

	change := PresenceChange{
		Type:     PresenceUpdate,
		Channel:  channel,
		ClientID: clientID,
		State:    state,
	}
	for _, cb := range callbacks {
		cb(change)
	}
}

// GetPresence returns all entries currently in a channel.
func (pm *PresenceManager) GetPresence(channel string) []PresenceEntry {
	pm.mu.RLock()
	ch, ok := pm.channels[channel]
	pm.mu.RUnlock()
	if !ok {
		return nil
	}

	ch.mu.RLock()
	defer ch.mu.RUnlock()

	result := make([]PresenceEntry, 0, len(ch.entries))
	for _, entry := range ch.entries {
		result = append(result, *entry)
	}
	return result
}

// OnChange registers a callback for presence changes on a channel.
// Returns a function that unregisters the callback.
func (pm *PresenceManager) OnChange(channel string, callback PresenceCallback) func() {
	ch := pm.getOrCreateChannel(channel)

	ch.mu.Lock()
	idx := len(ch.callbacks)
	ch.callbacks = append(ch.callbacks, callback)
	ch.mu.Unlock()

	return func() {
		ch.mu.Lock()
		defer ch.mu.Unlock()
		if idx < len(ch.callbacks) {
			ch.callbacks = append(ch.callbacks[:idx], ch.callbacks[idx+1:]...)
		}
	}
}

// ChannelCount returns the number of clients in a channel.
func (pm *PresenceManager) ChannelCount(channel string) int {
	pm.mu.RLock()
	ch, ok := pm.channels[channel]
	pm.mu.RUnlock()
	if !ok {
		return 0
	}

	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return len(ch.entries)
}

// MarshalPresence returns the JSON-encoded presence for a channel.
func (pm *PresenceManager) MarshalPresence(channel string) ([]byte, error) {
	entries := pm.GetPresence(channel)
	return json.Marshal(entries)
}
