package providerpreset

import "testing"

func TestCorrectLegacyVideoProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, baseURL, model, configured, want string
	}{
		{"fast five-reference incident", "https://www.whatstoken.ai/v1", "seedance-2.0-fast", "newapi", "video-generations-json"},
		{"root endpoint", "https://whatstoken.ai/", "seedance-2.0", "newapi", "video-generations-json"},
		{"normalized endpoint", " https://WWW.WHATSTOKEN.AI:443/v1/ ", " SEEDANCE-2.0-FAST ", "newapi", "video-generations-json"},
		{"unknown gateway", "https://provider.example/v1", "seedance-2.0-fast", "newapi", "newapi"},
		{"lookalike host", "https://whatstoken.ai.example/v1", "seedance-2.0-fast", "newapi", "newapi"},
		{"unverified subdomain", "https://api.whatstoken.ai/v1", "seedance-2.0-fast", "newapi", "newapi"},
		{"unknown suffix", "https://whatstoken.ai/v1", "seedance-2.0-fast-custom", "newapi", "newapi"},
		{"other model", "https://whatstoken.ai/v1", "sora-2", "newapi", "newapi"},
		{"explicit native protocol", "https://whatstoken.ai/v1", "seedance-2.0-fast", "volcengine-ark-video", "volcengine-ark-video"},
		{"explicit JSON protocol", "https://whatstoken.ai/v1", "seedance-2.0-fast", "video-generations-json", "video-generations-json"},
		{"missing protocol", "https://whatstoken.ai/v1", "seedance-2.0-fast", "", ""},
		{"custom prefix", "https://whatstoken.ai/proxy/v1", "seedance-2.0-fast", "newapi", "newapi"},
		{"encoded prefix", "https://whatstoken.ai/%76%31", "seedance-2.0-fast", "newapi", "newapi"},
		{"credentials", "https://user@whatstoken.ai/v1", "seedance-2.0-fast", "newapi", "newapi"},
		{"query", "https://whatstoken.ai/v1?route=other", "seedance-2.0-fast", "newapi", "newapi"},
		{"fragment", "https://whatstoken.ai/v1#other", "seedance-2.0-fast", "newapi", "newapi"},
		{"alternate port", "https://whatstoken.ai:8443/v1", "seedance-2.0-fast", "newapi", "newapi"},
		{"insecure", "http://whatstoken.ai/v1", "seedance-2.0-fast", "newapi", "newapi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CorrectLegacyVideoProtocol(tc.baseURL, tc.model, tc.configured); got != tc.want {
				t.Fatalf("protocol = %q, want %q", got, tc.want)
			}
		})
	}
}
