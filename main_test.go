package main

import "testing"

func TestThemeMatches(t *testing.T) {
	tests := []struct {
		id       string
		reported string
		want     bool
	}{
		{id: "PINK_PONY", reported: "Pink Pony Club", want: true},
		{id: "OCEAN_WAVES", reported: "Ocean Waves", want: true},
		{id: "RAINBOW", reported: "rainbow", want: true},
		{id: "GREEN", reported: "Forest", want: false},
		{id: "", reported: "Forest", want: false},
	}

	for _, test := range tests {
		if got := themeMatches(test.id, test.reported); got != test.want {
			t.Errorf("themeMatches(%q, %q) = %v, want %v", test.id, test.reported, got, test.want)
		}
	}
}
