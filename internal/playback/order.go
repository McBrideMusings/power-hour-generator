// Package playback owns the materialized playback order — the authoritative
// list of slots (collection rows and inline files) that make up a project's
// final concatenated output. Locking, swapping and shuffling are all
// statements about positions in this order, so the order has to exist as
// data rather than being recomputed from config + plan-file row order on
// every run.
//
// Per ADR 0003, this package is the single domain implementation both the
// CLI and the TUI dashboard call — no order/reconcile logic belongs in
// internal/cli or internal/tui/dashboard.
package playback

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FileName is the name of the materialized playback order file. It lives at
// the project root (NOT .powerhour/, which is derived state the `clean`
// command may delete) because the order is authoritative, hand-editable
// data.
const FileName = "playback-order.yaml"

// Order is the materialized playback order: an authoritative, ordered list
// of slots.
type Order struct {
	Version int    `yaml:"version"`
	Slots   []Slot `yaml:"slots"`
}

// Slot is a single position in the playback order. Exactly one of
// Collection or File is set: a collection slot names the pool it draws from
// and the stable row id it resolved to; a file slot names the inline source
// file and has no pool at all (structurally, not by lock) so it never
// participates in swaps or shuffles.
type Slot struct {
	Collection string `yaml:"collection,omitempty"` // empty for file slots
	RowID      string `yaml:"id,omitempty"`         // stable id from the plan file
	File       string `yaml:"file,omitempty"`
	Locked     bool   `yaml:"locked,omitempty"`
}

// Path returns the absolute path to the playback order file within
// projectRoot.
func Path(projectRoot string) string {
	return filepath.Join(projectRoot, FileName)
}

// Load reads the playback order from projectRoot. found is false (with a nil
// error) when no order file exists yet — the caller should materialize one
// instead of treating this as a failure.
func Load(projectRoot string) (Order, bool, error) {
	data, err := os.ReadFile(Path(projectRoot))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Order{}, false, nil
		}
		return Order{}, false, fmt.Errorf("read playback order: %w", err)
	}

	var order Order
	if err := yaml.Unmarshal(data, &order); err != nil {
		return Order{}, false, fmt.Errorf("parse playback order: %w", err)
	}

	return order, true, nil
}

// Save writes the playback order to projectRoot using an atomic write (temp
// file + rename), mirroring csvplan.WriteYAML.
func Save(projectRoot string, o Order) error {
	data, err := yaml.Marshal(o)
	if err != nil {
		return fmt.Errorf("marshal playback order: %w", err)
	}

	dir := projectRoot
	tmp, err := os.CreateTemp(dir, ".playback-order-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write playback order: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, Path(projectRoot)); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}
