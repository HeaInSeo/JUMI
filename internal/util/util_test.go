package util

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{
			name:  "first is non-empty",
			input: []string{"a", "b", "c"},
			want:  "a",
		},
		{
			name:  "first two are empty",
			input: []string{"", "", "c"},
			want:  "c",
		},
		{
			name:  "all empty",
			input: []string{"", "", ""},
			want:  "",
		},
		{
			name:  "no args",
			input: []string{},
			want:  "",
		},
		{
			name:  "single non-empty",
			input: []string{"hello"},
			want:  "hello",
		},
		{
			name:  "single empty",
			input: []string{""},
			want:  "",
		},
		{
			name:  "empty then non-empty",
			input: []string{"", "first-non-empty"},
			want:  "first-non-empty",
		},
		{
			name:  "non-empty then empty",
			input: []string{"first", ""},
			want:  "first",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FirstNonEmpty(tc.input...)
			if got != tc.want {
				t.Fatalf("FirstNonEmpty(%v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
