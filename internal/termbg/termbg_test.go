package termbg

import "testing"

func TestDark(t *testing.T) {
	for v, want := range map[string]bool{"": true, "15;0": true, "0;15": false, "0;7": false, "7;8": true, "default;default": true, "15;default;0": true, "12;11": false} {
		t.Setenv("COLORFGBG", v)
		if got := Dark(); got != want {
			t.Errorf("COLORFGBG=%q: Dark() = %v, want %v", v, got, want)
		}
	}
}
