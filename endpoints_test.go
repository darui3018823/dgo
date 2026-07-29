package dgo

import "testing"

func TestEndpointVoiceHasSinglePathSeparators(t *testing.T) {
	want := EndpointAPI + "voice/"
	if EndpointVoice != want {
		t.Fatalf("EndpointVoice = %q, want %q", EndpointVoice, want)
	}
	if EndpointVoiceRegions != want+"regions" {
		t.Fatalf("EndpointVoiceRegions = %q, want %q", EndpointVoiceRegions, want+"regions")
	}
}
