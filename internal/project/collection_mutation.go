package project

import "powerhour/pkg/csvplan"

// CollectionOptionsForConfig converts collection config into csvplan options.
func CollectionOptionsForConfig(cfg Collection) csvplan.CollectionOptions {
	defaultDuration := 60
	if cfg.Config.Duration > 0 {
		defaultDuration = cfg.Config.Duration
	}
	return csvplan.CollectionOptions{
		LinkHeader:      cfg.Config.LinkHeader,
		StartHeader:     cfg.Config.StartHeader,
		DurationHeader:  cfg.Config.DurationHeader,
		DefaultDuration: defaultDuration,
	}
}

// AppendCollectionRows appends imported rows, reindexes the combined set, and
// expands CSV headers to preserve any new dynamic fields.
func AppendCollectionRows(coll Collection, imported []csvplan.CollectionRow) Collection {
	rows := make([]csvplan.CollectionRow, 0, len(coll.Rows)+len(imported))
	rows = append(rows, coll.Rows...)
	rows = append(rows, imported...)
	for i := range rows {
		rows[i].Index = i + 1
	}
	coll.Rows = rows
	coll.Headers = csvplan.MergeHeaders(coll.Headers, rows)
	return coll
}

// WriteCollectionPlan persists a collection back to its configured plan file.
func WriteCollectionPlan(coll Collection) error {
	if coll.PlanFormat == "yaml" {
		return csvplan.WriteYAML(coll.Plan, coll.Headers, coll.Rows)
	}
	delimiter := coll.Delimiter
	if delimiter == 0 {
		delimiter = ','
	}
	headers := coll.Headers
	if len(headers) == 0 {
		headers = csvplan.MergeHeaders(nil, coll.Rows)
	}
	return csvplan.WriteCSV(coll.Plan, headers, coll.Rows, delimiter)
}
