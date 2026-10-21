package analysis

import "testing"

func TestValidate(t *testing.T) {
	steps := map[string]string{"s1": "STEP1", "s2": "STEP2"}
	hunks := map[string]string{"h1": "HUNK1"}
	sf := func(a string) string { return steps[a] }
	hf := func(a string) string { return hunks[a] }
	cases := []struct{ in, want string }{
		{"It adds a retry [step:s1]. Everything is safe. It fixes the bug [hunk:h1].", "It adds a retry [step:STEP1]. It fixes the bug [hunk:HUNK1]."},
		{"Made up [step:s9].", NoEvidence},
		{"Uncited only. Still uncited!", NoEvidence},
		{"- first [step:s2]\n- second\n## Heading\nText. [step:s1]", "- first [step:STEP2]\nText. [step:STEP1]"},
		{"Two cites [step:s1] [step:s9] here.", "Two cites [step:STEP1] here."},
	}
	for _, c := range cases {
		got, _ := Validate(c.in, sf, hf, false)
		if got != c.want {
			t.Errorf("Validate(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	got, cites := Validate("Not in the session record. Check the CI logs.", sf, hf, true)
	if got != "Not in the session record." || len(cites.Steps) != 0 {
		t.Errorf("not in record: %q", got)
	}
	_, cites = Validate("A [step:s1]. B [step:s1] [hunk:h1].", sf, hf, false)
	if len(cites.Steps) != 1 || len(cites.Hunks) != 1 {
		t.Errorf("cites = %+v", cites)
	}
}

func TestQuoteIn(t *testing.T) {
	if !QuoteIn("wrap refresh  in a retry", "I will wrap refresh in a\nretry so it works.") {
		t.Error("whitespace-insensitive match failed")
	}
	if QuoteIn("invented quote", "I will wrap refresh") || QuoteIn("", "x") {
		t.Error("false match")
	}
}
