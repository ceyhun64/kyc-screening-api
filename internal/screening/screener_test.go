package screening

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"upper case and trimmed", "  viktor   blackwood ", "BLACKWOOD VIKTOR"},
		{"turkish letters", "Ayşe Yılmaz", "AYSE YILMAZ"},
		{"turkish capital I with dot", "İSMAİL ÇELİK", "CELIK ISMAIL"},
		{"order does not matter", "Yılmaz, Ayşe", "AYSE YILMAZ"},
		{"hyphen splits parts", "Elena Marsh-Novak", "ELENA MARSH NOVAK"},
		{"digits and symbols removed", "John Smith 3rd!", "JOHN RD SMITH"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.in); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestScreen(t *testing.T) {
	s := New(DemoList, DemoListVersion, 0.85)

	tests := []struct {
		name      string
		in        string
		wantMatch bool
		wantName  string
	}{
		{"exact match", "Viktor Blackwood", true, "Viktor Blackwood"},
		{"reversed order", "Blackwood Viktor", true, "Viktor Blackwood"},
		{"written without turkish letters", "KEMAL YILDIRIMOGLU", true, "Kemal Yıldırımoğlu"},
		{"small typo", "Viktor Blakwood", true, "Viktor Blackwood"},
		{"different person", "Ayşe Yılmaz", false, ""},
		{"only first name shared", "Viktor Hugo", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Screen(tt.in)
			if got.Match != tt.wantMatch {
				t.Fatalf("Screen(%q).Match = %v (score %.3f), want %v", tt.in, got.Match, got.Score, tt.wantMatch)
			}
			if got.MatchedName != tt.wantName {
				t.Errorf("Screen(%q).MatchedName = %q, want %q", tt.in, got.MatchedName, tt.wantName)
			}
			if got.ListVersion != DemoListVersion {
				t.Errorf("ListVersion = %q, want %q", got.ListVersion, DemoListVersion)
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"ABC", "", 3},
		{"KITTEN", "SITTING", 3},
		{"BLACKWOOD", "BLAKWOOD", 1},
	}
	for _, tt := range tests {
		if got := levenshtein(tt.a, tt.b); got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
