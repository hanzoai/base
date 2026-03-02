// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"sync"
	"sync/atomic"
)

// UsageReport holds operation counts for a vault.
type UsageReport struct {
	VaultID string `json:"vaultId"`
	Puts    int64  `json:"puts"`
	Gets    int64  `json:"gets"`
	Syncs   int64  `json:"syncs"`
	Anchors int64  `json:"anchors"`
}

// Meter tracks per-vault operation counts.
// Designed for pay-per-use billing but works locally too.
type Meter struct {
	mu     sync.RWMutex
	vaults map[string]*vaultCounters
}

type vaultCounters struct {
	puts    atomic.Int64
	gets    atomic.Int64
	syncs   atomic.Int64
	anchors atomic.Int64
}

// NewMeter creates a new usage meter.
func NewMeter() *Meter {
	return &Meter{
		vaults: make(map[string]*vaultCounters),
	}
}

func (m *Meter) counters(vaultID string) *vaultCounters {
	m.mu.RLock()
	c, ok := m.vaults[vaultID]
	m.mu.RUnlock()
	if ok {
		return c
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check after write lock.
	if c, ok := m.vaults[vaultID]; ok {
		return c
	}
	c = &vaultCounters{}
	m.vaults[vaultID] = c
	return c
}

// RecordPut increments the put counter for a vault.
func (m *Meter) RecordPut(vaultID string) {
	m.counters(vaultID).puts.Add(1)
}

// RecordGet increments the get counter for a vault.
func (m *Meter) RecordGet(vaultID string) {
	m.counters(vaultID).gets.Add(1)
}

// RecordSync increments the sync counter for a vault.
func (m *Meter) RecordSync(vaultID string) {
	m.counters(vaultID).syncs.Add(1)
}

// RecordAnchor increments the anchor counter for a vault.
func (m *Meter) RecordAnchor(vaultID string) {
	m.counters(vaultID).anchors.Add(1)
}

// GetUsage returns the usage report for a vault.
func (m *Meter) GetUsage(vaultID string) *UsageReport {
	c := m.counters(vaultID)
	return &UsageReport{
		VaultID: vaultID,
		Puts:    c.puts.Load(),
		Gets:    c.gets.Load(),
		Syncs:   c.syncs.Load(),
		Anchors: c.anchors.Load(),
	}
}

// Reset zeroes all counters for a vault.
func (m *Meter) Reset(vaultID string) {
	c := m.counters(vaultID)
	c.puts.Store(0)
	c.gets.Store(0)
	c.syncs.Store(0)
	c.anchors.Store(0)
}
