package i18n

import "testing"

func TestDetectFrom(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"empty env defaults to English", nil, "en"},
		{"DMAIL_LANG wins over LANG", map[string]string{"DMAIL_LANG": "es", "LANG": "en_US.UTF-8"}, "es"},
		{"LC_ALL beats LC_MESSAGES", map[string]string{"LC_ALL": "es_MX.UTF-8", "LC_MESSAGES": "en_US.UTF-8"}, "es"},
		{"LANG as last resort", map[string]string{"LANG": "es_ES.UTF-8"}, "es"},
		{"hyphen and region stripped", map[string]string{"DMAIL_LANG": "es-419"}, "es"},
		{"pt_BR narrows to pt", map[string]string{"LANG": "pt_BR.UTF-8"}, "pt"},
		{"modifier stripped", map[string]string{"LANG": "ca_ES@valencia"}, "ca"},
		{"C locale falls through to English table", map[string]string{"LANG": "C.UTF-8"}, "c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectFrom(func(k string) string { return tc.env[k] })
			if got != tc.want {
				t.Fatalf("detectFrom() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTFor(t *testing.T) {
	if got := tFor("es", "Mark read"); got != "Marcar leído" {
		t.Fatalf("es override = %q", got)
	}
	if got := tFor("pt", "Mark read"); got != "Marcar como lida" {
		t.Fatalf("pt override = %q", got)
	}
	if got := tFor("en", "Mark read"); got != "Mark read" {
		t.Fatalf("English source = %q", got)
	}
	// Unknown language and unknown key both fall back to the source string.
	if got := tFor("c", "Mark read"); got != "Mark read" {
		t.Fatalf("unknown lang = %q", got)
	}
	if got := tFor("es", "No such key"); got != "No such key" {
		t.Fatalf("unknown key = %q", got)
	}
}

// Every language table translates the same set of source strings, so a
// string added to one locale cannot silently stay English in another.
func TestOverridesCoverSameKeys(t *testing.T) {
	var reference string
	for lang := range overrides {
		if reference == "" || lang < reference {
			reference = lang
		}
	}
	for lang, table := range overrides {
		if lang == reference {
			continue
		}
		for key := range overrides[reference] {
			if _, ok := table[key]; !ok {
				t.Errorf("%q is missing the %q translation", lang, key)
			}
		}
		for key := range table {
			if _, ok := overrides[reference][key]; !ok {
				t.Errorf("%q translates %q, which %q does not", lang, key, reference)
			}
		}
	}
}
