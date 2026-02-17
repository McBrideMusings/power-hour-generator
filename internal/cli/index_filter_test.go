package cli

import (
	"testing"

	"powerhour/pkg/csvplan"
)

func TestParseIndexArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []int
		wantErr bool
	}{
		{"single index", []string{"3"}, []int{3}, false},
		{"multiple singles", []string{"1", "3", "5"}, []int{1, 3, 5}, false},
		{"range", []string{"2-4"}, []int{2, 3, 4}, false},
		{"mixed singles and ranges", []string{"1", "3-5", "7"}, []int{1, 3, 4, 5, 7}, false},
		{"whitespace trimmed", []string{" 2 ", " 3 - 5 "}, []int{2, 3, 4, 5}, false},
		{"empty strings skipped", []string{"", "2", ""}, []int{2}, false},
		{"single element range", []string{"5-5"}, []int{5}, false},
		{"zero index rejected", []string{"0"}, nil, true},
		{"negative in range rejected", []string{"0-3"}, nil, true},
		{"reversed range rejected", []string{"5-2"}, nil, true},
		{"non-numeric rejected", []string{"abc"}, nil, true},
		{"non-numeric range start", []string{"x-3"}, nil, true},
		{"non-numeric range end", []string{"3-y"}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIndexArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index[%d] = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFilterRowsByIndex(t *testing.T) {
	rows := []csvplan.Row{
		{Index: 1, Title: "one"},
		{Index: 2, Title: "two"},
		{Index: 3, Title: "three"},
		{Index: 4, Title: "four"},
		{Index: 5, Title: "five"},
	}

	tests := []struct {
		name       string
		indexes    []int
		wantTitles []string
		wantErr    bool
	}{
		{"single match", []int{3}, []string{"three"}, false},
		{"multiple matches", []int{1, 4, 5}, []string{"one", "four", "five"}, false},
		{"preserves order from rows", []int{5, 1}, []string{"one", "five"}, false},
		{"missing index errors", []int{1, 99}, nil, true},
		{"zero index errors", []int{0}, nil, true},
		{"empty indexes errors", []int{}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := filterRowsByIndex(rows, tt.indexes)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.wantTitles) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.wantTitles))
			}
			for i, row := range got {
				if row.Title != tt.wantTitles[i] {
					t.Errorf("row[%d].Title = %q, want %q", i, row.Title, tt.wantTitles[i])
				}
			}
		})
	}
}

func TestFilterRowsByIndexArgs(t *testing.T) {
	rows := []csvplan.Row{
		{Index: 1, Title: "one"},
		{Index: 2, Title: "two"},
		{Index: 3, Title: "three"},
		{Index: 4, Title: "four"},
		{Index: 5, Title: "five"},
	}

	t.Run("empty args returns all rows", func(t *testing.T) {
		got, err := filterRowsByIndexArgs(rows, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != len(rows) {
			t.Fatalf("len = %d, want %d", len(got), len(rows))
		}
	})

	t.Run("range and singles", func(t *testing.T) {
		got, err := filterRowsByIndexArgs(rows, []string{"2-3", "5"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"two", "three", "five"}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d", len(got), len(want))
		}
		for i, row := range got {
			if row.Title != want[i] {
				t.Errorf("row[%d].Title = %q, want %q", i, row.Title, want[i])
			}
		}
	})

	t.Run("invalid arg propagates error", func(t *testing.T) {
		_, err := filterRowsByIndexArgs(rows, []string{"0"})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing index propagates error", func(t *testing.T) {
		_, err := filterRowsByIndexArgs(rows, []string{"99"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
