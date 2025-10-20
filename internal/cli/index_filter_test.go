package cli

import (
	"reflect"
	"testing"

	"powerhour/pkg/csvplan"
)

func TestFilterRowsByIndexArgs(t *testing.T) {
	rows := []csvplan.Row{
		{Index: 1, Title: "one"},
		{Index: 2, Title: "two"},
		{Index: 3, Title: "three"},
		{Index: 4, Title: "four"},
		{Index: 5, Title: "five"},
	}

	filtered, err := filterRowsByIndexArgs(rows, []string{"2-3", "5"})
	if err != nil {
		t.Fatalf("filterRowsByIndexArgs returned error: %v", err)
	}

	want := []csvplan.Row{rows[1], rows[2], rows[4]}
	if !reflect.DeepEqual(filtered, want) {
		t.Fatalf("filtered rows = %+v, want %+v", filtered, want)
	}
}

func TestFilterRowsByIndexArgsInvalid(t *testing.T) {
	rows := []csvplan.Row{
		{Index: 1},
	}

	if _, err := filterRowsByIndexArgs(rows, []string{"0"}); err == nil {
		t.Fatal("expected error for invalid index 0")
	}
}
