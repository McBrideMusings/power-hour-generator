package cli

import "powerhour/pkg/csvplan"

// filterRowsByIndexArgs trims the rows slice to those matching the provided
// CLI index arguments. When args is empty, the original rows are returned.
func filterRowsByIndexArgs(rows []csvplan.Row, args []string) ([]csvplan.Row, error) {
	if len(args) == 0 {
		return rows, nil
	}

	indexes, err := parseIndexArgs(args)
	if err != nil {
		return nil, err
	}

	return filterRowsByIndex(rows, indexes)
}
