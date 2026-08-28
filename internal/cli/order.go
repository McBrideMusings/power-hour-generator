package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/playback"
	"powerhour/internal/project"
)

// orderShuffleCollection is the --collection flag on `order shuffle`,
// mirroring the package-level flag-var pattern used by clean's --dry-run.
var orderShuffleCollection string

func newOrderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "order",
		Short: "Inspect and mutate the playback order",
		RunE:  runOrderList,
	}

	cmd.AddCommand(newOrderSwapCmd())
	cmd.AddCommand(newOrderSetCmd())
	cmd.AddCommand(newOrderLockCmd())
	cmd.AddCommand(newOrderUnlockCmd())
	cmd.AddCommand(newOrderShuffleCmd())
	cmd.AddCommand(newOrderReconcileCmd())

	return cmd
}

func newOrderSwapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "swap <slotA> <slotB>",
		Short: "Swap the occupants of two playback-order slots",
		Args:  cobra.ExactArgs(2),
		RunE:  runOrderSwap,
	}
}

func newOrderSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <slot> <row-id>",
		Short: "Assign a specific row id to a playback-order slot",
		Args:  cobra.ExactArgs(2),
		RunE:  runOrderSet,
	}
}

func newOrderLockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lock <slot>",
		Short: "Lock a playback-order slot so shuffle skips it",
		Args:  cobra.ExactArgs(1),
		RunE:  runOrderLock,
	}
}

func newOrderUnlockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock <slot>",
		Short: "Unlock a playback-order slot",
		Args:  cobra.ExactArgs(1),
		RunE:  runOrderUnlock,
	}
}

func newOrderShuffleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shuffle",
		Short: "Shuffle playback-order slots (all collections, or one with --collection)",
		Args:  cobra.NoArgs,
		RunE:  runOrderShuffle,
	}
	cmd.Flags().StringVar(&orderShuffleCollection, "collection", "", "Shuffle only this collection's slots")
	return cmd
}

func newOrderReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile the stored playback order against the current timeline and pools",
		Args:  cobra.NoArgs,
		RunE:  runOrderReconcile,
	}
}

// orderSlotOutput is the JSON/table-shared per-slot view: slot number,
// collection, row id, resolved display label, lock state, and file path for
// file slots.
type orderSlotOutput struct {
	Slot       int    `json:"slot"`
	Kind       string `json:"kind"` // "collection" | "file"
	Collection string `json:"collection,omitempty"`
	RowID      string `json:"row_id,omitempty"`
	Label      string `json:"label"`
	Locked     bool   `json:"locked"`
	File       string `json:"file,omitempty"`
}

// orderChangeOutput is the JSON-serializable form of a playback.Change.
type orderChangeOutput struct {
	Kind       string `json:"kind"`
	Collection string `json:"collection,omitempty"`
	RowID      string `json:"row_id,omitempty"`
	File       string `json:"file,omitempty"`
	Detail     string `json:"detail"`
}

// loadOrderForMutation runs the shared prologue every order subcommand
// starts from: resolve the project, load config + collections, load (or
// materialize) the stored order, and reconcile it against the current
// timeline. Reconcile always runs — callers must report changes before
// acting, per playback-order.yaml's authoritative-but-reconciled contract.
func loadOrderForMutation() (paths.ProjectPaths, config.Config, map[string]project.Collection, playback.Order, []playback.Change, error) {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return pp, config.Config{}, nil, playback.Order{}, nil, err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return pp, cfg, nil, playback.Order{}, nil, err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	if len(cfg.Collections) == 0 {
		return pp, cfg, nil, playback.Order{}, nil, fmt.Errorf("no collections configured")
	}

	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return pp, cfg, nil, playback.Order{}, nil, err
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		return pp, cfg, nil, playback.Order{}, nil, err
	}

	prev, found, err := playback.Load(pp.Root)
	if err != nil {
		return pp, cfg, collections, playback.Order{}, nil, err
	}
	if !found {
		prev, err = playback.Materialize(cfg, collections)
		if err != nil {
			return pp, cfg, collections, playback.Order{}, nil, err
		}
	}

	order, changes, err := playback.Reconcile(prev, cfg, collections)
	if err != nil {
		return pp, cfg, collections, playback.Order{}, nil, err
	}

	return pp, cfg, collections, order, changes, nil
}

// parseSlotArg converts a 1-based CLI slot argument into a 0-based core
// index, bounds-checking against total (the order's current slot count) so
// the error has the same shape as the core's own out-of-range error.
func parseSlotArg(s string, total int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid slot number %q", s)
	}
	if n < 1 || n > total {
		return 0, fmt.Errorf("slot %d out of range (order has %d slots)", n, total)
	}
	return n - 1, nil
}

// buildOrderSlotOutputs is the single place slot rows are shaped for both
// the table and --json — every subcommand's output goes through it so
// listing and mutation report the identical shape.
func buildOrderSlotOutputs(o playback.Order, collections map[string]project.Collection) []orderSlotOutput {
	out := make([]orderSlotOutput, len(o.Slots))
	for i, s := range o.Slots {
		row := orderSlotOutput{Slot: i + 1, Locked: s.Locked}
		if s.File != "" {
			row.Kind = "file"
			row.File = s.File
			row.Label = project.FallbackLabel(s.File)
		} else {
			row.Kind = "collection"
			row.Collection = s.Collection
			row.RowID = s.RowID
			row.Label = labelForCollectionRow(collections, s.Collection, s.RowID)
		}
		out[i] = row
	}
	return out
}

// labelForCollectionRow resolves a slot's display label via
// project.CollectionRowLabel — never by branching on column names (ADR
// 0002). Falls back to the bare collection name when the row can no longer
// be found (a stale order slot whose row was since deleted).
func labelForCollectionRow(collections map[string]project.Collection, collection, rowID string) string {
	coll, ok := collections[collection]
	if !ok {
		return collection
	}
	for _, row := range coll.Rows {
		if row.RowID == rowID {
			return project.CollectionRowLabel(coll.Config, row)
		}
	}
	return collection
}

// collectionHasRowID reports whether rowID names an actual row in coll's
// pool, so `order set` can fail with a useful message instead of writing an
// id that resolves to nothing.
func collectionHasRowID(coll project.Collection, rowID string) bool {
	for _, row := range coll.Rows {
		if row.RowID == rowID {
			return true
		}
	}
	return false
}

func buildOrderChangeOutputs(changes []playback.Change) []orderChangeOutput {
	out := make([]orderChangeOutput, len(changes))
	for i, c := range changes {
		out[i] = orderChangeOutput{
			Kind:       string(c.Kind),
			Collection: c.Collection,
			RowID:      c.RowID,
			File:       c.File,
			Detail:     c.Detail,
		}
	}
	return out
}

func printOrderChanges(out io.Writer, changes []playback.Change) {
	for _, c := range changes {
		id := c.RowID
		if id == "" {
			id = c.File
		}
		label := c.Collection
		if label == "" {
			label = id
		} else if id != "" {
			label = label + " " + id
		}
		fmt.Fprintf(out, "  %s: %s — %s\n", c.Kind, label, c.Detail)
	}
}

func printOrderSlots(out io.Writer, slots []orderSlotOutput) {
	for _, s := range slots {
		lock := ""
		if s.Locked {
			lock = "  [locked]"
		}
		if s.Kind == "file" {
			fmt.Fprintf(out, "%4d  %-12s  %-8s  %s%s\n", s.Slot, "file", "", s.Label, lock)
		} else {
			fmt.Fprintf(out, "%4d  %-12s  %-8s  %s%s\n", s.Slot, s.Collection, s.RowID, s.Label, lock)
		}
	}
}

// reportOrderLoad is the shared tail for the read-only paths (bare `order`
// and `order reconcile`): persist the reconciled order when Reconcile
// produced changes, then print/emit changes followed by the slot listing.
func reportOrderLoad(cmd *cobra.Command, pp paths.ProjectPaths, collections map[string]project.Collection, order playback.Order, changes []playback.Change) error {
	if len(changes) > 0 {
		if err := playback.Save(pp.Root, order); err != nil {
			return fmt.Errorf("save playback order: %w", err)
		}
	}

	out := cmd.OutOrStdout()
	slots := buildOrderSlotOutputs(order, collections)

	if outputJSON {
		payload := struct {
			Changes []orderChangeOutput `json:"changes"`
			Slots   []orderSlotOutput   `json:"slots"`
		}{
			Changes: buildOrderChangeOutputs(changes),
			Slots:   slots,
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	if len(changes) > 0 {
		fmt.Fprintln(out, "Reconciled changes:")
		printOrderChanges(out, changes)
		fmt.Fprintln(out)
	}
	printOrderSlots(out, slots)
	return nil
}

// finalizeOrderMutation is the shared tail for every mutating subcommand:
// persist the order (the mutation itself, plus any reconcile changes),
// then print/emit the same changes+slots shape reportOrderLoad uses, with
// an action label so --json output can tell which mutation produced it.
func finalizeOrderMutation(cmd *cobra.Command, pp paths.ProjectPaths, collections map[string]project.Collection, order playback.Order, changes []playback.Change, action string) error {
	if err := playback.Save(pp.Root, order); err != nil {
		return fmt.Errorf("save playback order: %w", err)
	}

	out := cmd.OutOrStdout()
	slots := buildOrderSlotOutputs(order, collections)

	if outputJSON {
		payload := struct {
			Action  string              `json:"action"`
			Changes []orderChangeOutput `json:"changes"`
			Slots   []orderSlotOutput   `json:"slots"`
		}{
			Action:  action,
			Changes: buildOrderChangeOutputs(changes),
			Slots:   slots,
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	if len(changes) > 0 {
		fmt.Fprintln(out, "Reconciled changes:")
		printOrderChanges(out, changes)
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "%s applied.\n", action)
	printOrderSlots(out, slots)
	return nil
}

func runOrderList(cmd *cobra.Command, _ []string) error {
	glogf, gcloser := logx.StartCommand("order")
	defer gcloser.Close()
	glogf("order list started")

	pp, _, collections, order, changes, err := loadOrderForMutation()
	if err != nil {
		return err
	}
	glogf("order loaded: %d slots, %d reconcile changes", len(order.Slots), len(changes))

	return reportOrderLoad(cmd, pp, collections, order, changes)
}

func runOrderReconcile(cmd *cobra.Command, _ []string) error {
	glogf, gcloser := logx.StartCommand("order-reconcile")
	defer gcloser.Close()
	glogf("order reconcile started")

	pp, _, collections, order, changes, err := loadOrderForMutation()
	if err != nil {
		return err
	}
	glogf("order reconciled: %d slots, %d changes", len(order.Slots), len(changes))

	return reportOrderLoad(cmd, pp, collections, order, changes)
}

func runOrderSwap(cmd *cobra.Command, args []string) error {
	glogf, gcloser := logx.StartCommand("order-swap")
	defer gcloser.Close()

	pp, _, collections, order, changes, err := loadOrderForMutation()
	if err != nil {
		return err
	}

	a, err := parseSlotArg(args[0], len(order.Slots))
	if err != nil {
		return err
	}
	b, err := parseSlotArg(args[1], len(order.Slots))
	if err != nil {
		return err
	}

	if err := playback.Swap(&order, a, b); err != nil {
		return err
	}
	glogf("swapped slots %d and %d", a+1, b+1)

	return finalizeOrderMutation(cmd, pp, collections, order, changes, "swap")
}

func runOrderSet(cmd *cobra.Command, args []string) error {
	glogf, gcloser := logx.StartCommand("order-set")
	defer gcloser.Close()

	pp, _, collections, order, changes, err := loadOrderForMutation()
	if err != nil {
		return err
	}

	idx, err := parseSlotArg(args[0], len(order.Slots))
	if err != nil {
		return err
	}

	rowID := strings.TrimSpace(args[1])
	slot := order.Slots[idx]
	if slot.File != "" {
		return fmt.Errorf("slot %d is a file entry (%s): it has no collection or pool, so it holds its position", idx+1, slot.File)
	}
	coll, ok := collections[slot.Collection]
	if !ok || !collectionHasRowID(coll, rowID) {
		return fmt.Errorf("row id %q is not in collection %q", rowID, slot.Collection)
	}

	if err := playback.Set(&order, idx, rowID); err != nil {
		return err
	}
	glogf("set slot %d to row %s", idx+1, rowID)

	return finalizeOrderMutation(cmd, pp, collections, order, changes, "set")
}

func runOrderLock(cmd *cobra.Command, args []string) error {
	return runOrderSetLock(cmd, args, true)
}

func runOrderUnlock(cmd *cobra.Command, args []string) error {
	return runOrderSetLock(cmd, args, false)
}

func runOrderSetLock(cmd *cobra.Command, args []string, locked bool) error {
	action := "lock"
	if !locked {
		action = "unlock"
	}
	glogf, gcloser := logx.StartCommand("order-" + action)
	defer gcloser.Close()

	pp, _, collections, order, changes, err := loadOrderForMutation()
	if err != nil {
		return err
	}

	idx, err := parseSlotArg(args[0], len(order.Slots))
	if err != nil {
		return err
	}

	if err := playback.SetLock(&order, idx, locked); err != nil {
		return err
	}
	glogf("%s slot %d", action, idx+1)

	return finalizeOrderMutation(cmd, pp, collections, order, changes, action)
}

func runOrderShuffle(cmd *cobra.Command, _ []string) error {
	glogf, gcloser := logx.StartCommand("order-shuffle")
	defer gcloser.Close()

	pp, cfg, collections, order, changes, err := loadOrderForMutation()
	if err != nil {
		return err
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	name := strings.TrimSpace(orderShuffleCollection)
	if name != "" {
		coll, ok := collections[name]
		if !ok {
			return fmt.Errorf("collection %q is not configured", name)
		}
		if err := playback.Shuffle(&order, name, coll.Config.SelectionValue(), playback.Pool(coll), rng); err != nil {
			return err
		}
		glogf("shuffled collection %q", name)
	} else {
		if err := playback.ShuffleAll(&order, cfg, collections, rng); err != nil {
			return err
		}
		glogf("shuffled all collections")
	}

	return finalizeOrderMutation(cmd, pp, collections, order, changes, "shuffle")
}
