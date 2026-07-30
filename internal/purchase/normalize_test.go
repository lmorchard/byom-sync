package purchase

import "testing"

func TestFirstArtist(t *testing.T) {
	// Spotify joins collaborators with commas; the stores match a single name.
	// These strings are verbatim from the live hub.
	for _, tc := range []struct{ in, want string }{
		{"Cavedoll, Tim Phillips", "Cavedoll"},
		{"Sea Lemon, Benjamin Gibbard", "Sea Lemon"},
		{"Beach House", "Beach House"},
		{"  Spaced , Out ", "Spaced"},
		{"", ""},
	} {
		if got := FirstArtist(tc.in); got != tc.want {
			t.Errorf("FirstArtist(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCleanAlbum(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Crystals (feat. Benjamin Gibbard)", "Crystals"},
		{"Hellbilly Deluxe (Edited Version)", "Hellbilly Deluxe"},
		{"Fairytale (Deluxe Expanded Edition)", "Fairytale"},
		{"Sound Affects - Deluxe Edition", "Sound Affects"},
		{"The Queen Is Dead - 2011 Remaster", "The Queen Is Dead"},
		{"Once Twice Melody", "Once Twice Melody"},
		// Never strip down to nothing.
		{"(Untitled)", "(Untitled)"},
		{"", ""},
	} {
		if got := CleanAlbum(tc.in); got != tc.want {
			t.Errorf("CleanAlbum(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
