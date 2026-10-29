package generation

import (
	"testing"

	"infinite-canvas/backend/internal/modelcatalog"
)

func TestManagedVideoOnlySendsControlsDeclaredByServer(t *testing.T) {
	version := "server-v2"
	durationSupported := false
	profile := modelcatalog.DefaultModelCapabilityConfigForModel("openai-videos", "unseen-model").Video
	profile.Ratios, profile.Resolutions = nil, nil
	profile.DurationSupported = &durationSupported
	profile.GenerateAudio.Supported, profile.Watermark.Supported = false, false
	input := Input{Config: Config{Model: "seedance-2.5", VideoCapabilitiesVersion: &version, VideoSeconds: "8", Size: "16:9", VQuality: "1080p", VideoGenerateAudio: "true", VideoWatermark: "true"}, VideoCapability: profile}
	got := managedLegacyVideoInput(input)
	if got.GenerateAudio != nil || got.Watermark != nil || got.Seconds != "" || got.AspectRatio != "" || got.Resolution != "" {
		t.Fatal("stale client settings restored unsupported server controls")
	}
	profile.GenerateAudio.Supported, profile.Watermark.Supported = true, true
	input.Config.VideoGenerateAudio, input.Config.VideoWatermark = "false", "false"
	got = managedLegacyVideoInput(input)
	if got.GenerateAudio == nil || *got.GenerateAudio || got.Watermark == nil || *got.Watermark {
		t.Fatal("explicit false settings were discarded")
	}
	if taskInputFromCanvas(input).Config.VideoCapabilitiesVersion == nil {
		t.Fatal("generation validator lost the server capability version")
	}
}
