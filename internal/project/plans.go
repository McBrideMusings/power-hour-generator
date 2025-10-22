package project

import (
	"sort"

	"powerhour/pkg/csvplan"
)

// PlanRow couples a plan row with its clip type.
type PlanRow struct {
	ClipType ClipType
	Row      csvplan.Row
}

// FlattenPlans converts a map of plan rows into a stable slice.
func FlattenPlans(plans map[ClipType][]csvplan.Row) []PlanRow {
	if len(plans) == 0 {
		return nil
	}
	var flat []PlanRow

	ordered := []ClipType{
		ClipTypeSong,
		ClipTypeInterstitial,
		ClipTypeIntro,
		ClipTypeOutro,
	}

	seen := make(map[ClipType]struct{}, len(plans))

	appendRows := func(clipType ClipType) {
		rows, ok := plans[clipType]
		if !ok {
			return
		}
		seen[clipType] = struct{}{}
		for _, row := range rows {
			flat = append(flat, PlanRow{
				ClipType: clipType,
				Row:      row,
			})
		}
	}

	for _, clipType := range ordered {
		appendRows(clipType)
	}

	var remaining []ClipType
	for clipType := range plans {
		if _, ok := seen[clipType]; ok {
			continue
		}
		remaining = append(remaining, clipType)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return string(remaining[i]) < string(remaining[j])
	})
	for _, clipType := range remaining {
		appendRows(clipType)
	}

	return flat
}
