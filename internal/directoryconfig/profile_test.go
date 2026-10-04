package directoryconfig

import (
	"reflect"
	"testing"
)

func TestParseEnvironmentProfileNormalizesCompleteMapping(t *testing.T) {
	profile, err := parseEnvironmentProfile(`languages: [fr, en, en]
regions: []
notes: " note "
contact_url: https://relay.example/contact
`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile.Languages, []string{"en", "fr"}) || profile.Notes != "note" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestParseEnvironmentProfileRejectsUnknownField(t *testing.T) {
	if _, err := parseEnvironmentProfile("unknown: value\n"); err == nil {
		t.Fatal("expected error")
	}
}
