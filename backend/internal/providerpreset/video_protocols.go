package providerpreset

import (
	"net/url"
	"strings"
)

// CorrectLegacyVideoProtocol repairs the old Sora selection only for a verified
// endpoint/model contract. Other explicit protocols and unknown gateways keep
// their configured request shape; this never probes or retries an upstream.
func CorrectLegacyVideoProtocol(baseURL, model, configured string) string {
	if strings.TrimSpace(configured) != "newapi" {
		return configured
	}
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "seedance-2.0", "seedance-2.0-fast":
	default:
		return configured
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return configured
	}
	if host := strings.ToLower(u.Hostname()); host != "whatstoken.ai" && host != "www.whatstoken.ai" {
		return configured
	}
	if path := strings.TrimRight(u.EscapedPath(), "/"); path != "" && path != "/v1" {
		return configured
	}
	return "video-generations-json"
}
