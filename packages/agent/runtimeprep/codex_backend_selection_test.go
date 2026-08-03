package runtimeprep

import "testing"

func TestCodexNativeBundlesSuppressedOnlyForExplicitTuttiMode(t *testing.T) {
	if !codexNativeBundlesSuppressed(PrepareInput{
		BrowserBackendPreference:  CapabilityBackendPreferenceTutti,
		ComputerBackendPreference: CapabilityBackendPreferenceTutti,
		SitesBackendPreference:    CapabilityBackendPreferenceTutti,
	}) {
		t.Fatal("explicit Tutti preferences must suppress native bundle preparation")
	}
	if codexNativeBundlesSuppressed(PrepareInput{BrowserBackendPreference: CapabilityBackendPreferenceTutti}) {
		t.Fatal("partial preference must not suppress unrelated native preparation")
	}
}
