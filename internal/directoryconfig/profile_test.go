package directoryconfig

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEnvironmentProfileNormalizesCompleteMapping(t *testing.T) {
	profile, warnings, err := parseEnvironmentProfile(`participation_mode: open
languages: [fr, en, en]
regions: []
notes: " note "
contact_url: https://relay.example/contact
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if !reflect.DeepEqual(profile.Languages, []string{"en", "fr"}) || profile.Notes != "note" || profile.ParticipationMode != "open" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestParseEnvironmentProfileWarnsAndOmitsUnknownOrInvalidOptionalFields(t *testing.T) {
	profile, warnings, err := parseEnvironmentProfile(`participation_mode: unrestricted
unknown: value
contact_url: http://relay.example/contact
topics: [general]
`)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ParticipationMode != "" || profile.ContactURL != "" || !reflect.DeepEqual(profile.Topics, []string{"general"}) {
		t.Fatalf("profile = %#v", profile)
	}
	if len(warnings) != 3 {
		t.Fatalf("warnings = %#v, want 3", warnings)
	}
	joined := warnings[0].String() + "\n" + warnings[1].String() + "\n" + warnings[2].String()
	if !strings.Contains(joined, "allowed values are open, restricted, closed") || !strings.Contains(joined, "unknown optional profile key") {
		t.Fatalf("warnings = %s", joined)
	}
}
