package httpsignature

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryDestinationCapabilityStore struct {
	capability DestinationCapability
	found      bool
	save       bool
	err        error
}

func (store *memoryDestinationCapabilityStore) LoadDestinationCapability(
	context.Context,
	DestinationScope,
	string,
) (DestinationCapability, bool, error) {
	return store.capability, store.found, store.err
}

func (store *memoryDestinationCapabilityStore) SaveDestinationCapability(
	_ context.Context,
	capability DestinationCapability,
) (bool, error) {
	if store.err != nil {
		return false, store.err
	}
	store.capability = capability
	store.found = true
	if !store.save {
		return false, nil
	}
	return true, nil
}

func TestNormalizeDestinationOrigin(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "HTTPS://Example.COM:443/inbox?x=1#fragment",
			want:  "https://example.com",
		},
		{
			input: "http://Example.COM:80/actor",
			want:  "http://example.com",
		},
		{
			input: "https://Example.COM:8443/inbox",
			want:  "https://example.com:8443",
		},
		{
			input: "https://[2001:db8::1]:8443/inbox",
			want:  "https://[2001:db8::1]:8443",
		},
	}
	for _, test := range tests {
		got, err := NormalizeDestinationOrigin(test.input)
		if err != nil {
			t.Fatalf("normalize %q: %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf(
				"NormalizeDestinationOrigin(%q) = %q; want %q",
				test.input,
				got,
				test.want,
			)
		}
	}
}

func TestNormalizeDestinationOriginRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{
		"/relative",
		"ftp://example.com/inbox",
		"https://user@example.com/inbox",
		"https:///missing-host",
	} {
		if _, err := NormalizeDestinationOrigin(input); err == nil {
			t.Fatalf("invalid destination %q was accepted", input)
		}
	}
}

func testNegotiator(
	t *testing.T,
	store DestinationCapabilityStore,
	now time.Time,
) *DestinationNegotiator {
	t.Helper()

	negotiator, err := NewDestinationNegotiator(
		DestinationNegotiatorOptions{
			Store:       store,
			PositiveTTL: 14 * 24 * time.Hour,
			NegativeTTL: 24 * time.Hour,
			Now: func() time.Time {
				return now
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return negotiator
}

func TestFixedProfilesDoNotRequireCapabilityStore(t *testing.T) {
	negotiator := testNegotiator(
		t,
		nil,
		time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	)
	for _, profile := range []Profile{
		ProfileLegacy,
		ProfileRFC9421,
	} {
		plan, err := negotiator.Plan(
			context.Background(),
			profile,
			DestinationScopeDelivery,
			"https://remote.example/inbox",
		)
		if err != nil {
			t.Fatalf("plan fixed profile %q: %v", profile, err)
		}
		if plan.Primary != profile || plan.HasFallback() {
			t.Fatalf("fixed profile plan = %+v", plan)
		}
	}
}

func TestUnknownDualFetchUsesOnlyBoundedFallbackSignals(
	t *testing.T,
) {
	negotiator := testNegotiator(
		t,
		&memoryDestinationCapabilityStore{},
		time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	)
	plan, err := negotiator.Plan(
		context.Background(),
		ProfileDual,
		DestinationScopeFetch,
		"https://remote.example/actor",
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Primary != ProfileRFC9421 ||
		plan.Fallback != ProfileLegacy ||
		plan.Reason != NegotiationReasonUnknownFetchProbe {
		t.Fatalf("unknown fetch plan = %+v", plan)
	}
	for _, outcome := range []NegotiationOutcome{
		NegotiationOutcomeSuccess,
		NegotiationOutcomeTransportFailure,
		NegotiationOutcomeServerFailure,
		NegotiationOutcomeOtherFailure,
	} {
		if plan.ShouldFallback(outcome) {
			t.Fatalf("fallback allowed for outcome %q", outcome)
		}
	}
	for _, outcome := range []NegotiationOutcome{
		NegotiationOutcomeExplicitSignatureRejection,
		NegotiationOutcomeAmbiguousClientRejection,
	} {
		if !plan.ShouldFallback(outcome) {
			t.Fatalf("bounded fallback signal %q was rejected", outcome)
		}
	}
}

func TestUnknownDualDeliveryNeverFallsBack(t *testing.T) {
	negotiator := testNegotiator(
		t,
		&memoryDestinationCapabilityStore{},
		time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	)
	plan, err := negotiator.Plan(
		context.Background(),
		ProfileDual,
		DestinationScopeDelivery,
		"https://remote.example/inbox",
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Primary != ProfileLegacy ||
		plan.HasFallback() ||
		plan.Reason != NegotiationReasonUnknownDeliveryLegacy {
		t.Fatalf("unknown delivery plan = %+v", plan)
	}
	for _, outcome := range []NegotiationOutcome{
		NegotiationOutcomeExplicitSignatureRejection,
		NegotiationOutcomeAmbiguousClientRejection,
	} {
		if plan.ShouldFallback(outcome) {
			t.Fatalf("delivery plan permitted fallback for %q", outcome)
		}
	}
}

func TestDualUsesFreshCachedCapability(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	for _, profile := range []Profile{
		ProfileLegacy,
		ProfileRFC9421,
	} {
		evidence := CapabilityEvidenceExplicitRFC9421Rejection
		if profile == ProfileRFC9421 {
			evidence = CapabilityEvidenceSuccessfulRFC9421
		}
		store := &memoryDestinationCapabilityStore{
			found: true,
			capability: DestinationCapability{
				Origin:     "https://remote.example",
				Scope:      DestinationScopeDelivery,
				Profile:    profile,
				Evidence:   evidence,
				ObservedAt: now.Add(-time.Hour),
				ExpiresAt:  now.Add(time.Hour),
			},
		}
		plan, err := testNegotiator(t, store, now).Plan(
			context.Background(),
			ProfileDual,
			DestinationScopeDelivery,
			"https://remote.example/inbox",
		)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Primary != profile ||
			plan.HasFallback() ||
			plan.Reason != NegotiationReasonCachedCapability {
			t.Fatalf("cached plan = %+v", plan)
		}
	}
}

func TestExpiredCapabilityUsesUnknownRules(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	store := &memoryDestinationCapabilityStore{
		found: true,
		capability: DestinationCapability{
			Origin:     "https://remote.example",
			Scope:      DestinationScopeDelivery,
			Profile:    ProfileRFC9421,
			Evidence:   CapabilityEvidenceSuccessfulRFC9421,
			ObservedAt: now.Add(-48 * time.Hour),
			ExpiresAt:  now.Add(-24 * time.Hour),
		},
	}
	plan, err := testNegotiator(t, store, now).Plan(
		context.Background(),
		ProfileDual,
		DestinationScopeDelivery,
		"https://remote.example/inbox",
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Primary != ProfileLegacy ||
		plan.Reason != NegotiationReasonUnknownDeliveryLegacy {
		t.Fatalf("expired capability plan = %+v", plan)
	}
}

func TestCapabilityObservationsUseBoundedTTLs(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	store := &memoryDestinationCapabilityStore{save: true}
	negotiator := testNegotiator(t, store, now)

	positive, saved, err := negotiator.ObserveRFC9421Success(
		context.Background(),
		DestinationScopeDelivery,
		"https://remote.example/inbox",
		CapabilityEvidenceSuccessfulRFC9421,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !saved ||
		positive.Profile != ProfileRFC9421 ||
		positive.ExpiresAt.Sub(positive.ObservedAt) !=
			14*24*time.Hour {
		t.Fatalf("positive observation = %+v, saved=%v", positive, saved)
	}

	negative, saved, err :=
		negotiator.ObserveExplicitRFC9421Rejection(
			context.Background(),
			DestinationScopeFetch,
			"https://legacy.example/actor",
		)
	if err != nil {
		t.Fatal(err)
	}
	if !saved ||
		negative.Profile != ProfileLegacy ||
		negative.ExpiresAt.Sub(negative.ObservedAt) != 24*time.Hour {
		t.Fatalf("negative observation = %+v, saved=%v", negative, saved)
	}

	fallback, saved, err :=
		negotiator.ObserveSuccessfulLegacyFallback(
			context.Background(),
			DestinationScopeFetch,
			"https://nodebb.example/category/5",
		)
	if err != nil {
		t.Fatal(err)
	}
	if !saved ||
		fallback.Profile != ProfileLegacy ||
		fallback.Evidence != CapabilityEvidenceSuccessfulLegacyFallback ||
		fallback.ExpiresAt.Sub(fallback.ObservedAt) != 24*time.Hour {
		t.Fatalf("fallback observation = %+v, saved=%v", fallback, saved)
	}
}

func TestPositiveObservationRejectsInvalidEvidence(t *testing.T) {
	negotiator := testNegotiator(
		t,
		&memoryDestinationCapabilityStore{save: true},
		time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	)
	_, _, err := negotiator.ObserveRFC9421Success(
		context.Background(),
		DestinationScopeFetch,
		"https://remote.example/actor",
		CapabilityEvidenceExplicitRFC9421Rejection,
	)
	if err == nil {
		t.Fatal("invalid positive evidence was accepted")
	}
}

func TestDualRequiresStore(t *testing.T) {
	negotiator := testNegotiator(
		t,
		nil,
		time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	)
	_, err := negotiator.Plan(
		context.Background(),
		ProfileDual,
		DestinationScopeFetch,
		"https://remote.example/actor",
	)
	if err == nil {
		t.Fatal("dual planning without store succeeded")
	}
}

func TestNegotiatorPropagatesStoreError(t *testing.T) {
	want := errors.New("store unavailable")
	negotiator := testNegotiator(
		t,
		&memoryDestinationCapabilityStore{err: want},
		time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	)
	_, err := negotiator.Plan(
		context.Background(),
		ProfileDual,
		DestinationScopeFetch,
		"https://remote.example/actor",
	)
	if !errors.Is(err, want) {
		t.Fatalf("store error = %v; want %v", err, want)
	}
}
