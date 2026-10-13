package generation

import (
	"fmt"
	"strconv"
	"strings"
)

// H3's two images are keyframes, not Seedance subject references. The end
// frame may stand alone, and its canvas inherits the image's aspect ratio.
func beefAPIH3VideoBody(input Input) (map[string]interface{}, error) {
	if len(input.ReferenceImages) > 2 || len(input.ReferenceVideos) > 0 || len(input.ReferenceAudios) > 1 {
		return nil, fmt.Errorf("H3 Max Turbo 支持最多 2 张首尾帧、1 段配音，不支持参考视频")
	}
	seconds := strings.TrimSpace(input.Config.VideoSeconds)
	if seconds == "" {
		seconds = "5"
	}
	duration, err := strconv.Atoi(seconds)
	if err != nil || duration < 1 || duration > 15 {
		return nil, fmt.Errorf("H3 Max Turbo 视频时长须为 1–15 秒整数")
	}
	resolution := "768p"
	if strings.TrimSpace(input.Config.VQuality) != "" {
		resolution = NormalizeVideoResolution(input.Config.VQuality)
	}
	if resolution != "480p" && resolution != "768p" && resolution != "1080p" {
		return nil, fmt.Errorf("H3 Max Turbo 支持 480p、768p、1080p")
	}
	body := map[string]interface{}{"model": input.Config.Model, "prompt": strings.TrimSpace(input.Prompt), "seconds": seconds, "resolution": resolution}
	first, last := metadataString(input.Metadata, "videoStartFrameNodeId"), metadataString(input.Metadata, "videoEndFrameNodeId")
	for index, image := range input.ReferenceImages {
		url, err := openAIImageInputURL(image)
		if err != nil {
			return nil, err
		}
		if first != "" || last != "" {
			matched := false
			if image.ID == first {
				body["first_frame_url"] = url
				matched = true
			}
			if image.ID == last {
				body["last_frame_url"] = url
				matched = true
			}
			if !matched {
				return nil, fmt.Errorf("H3 Max Turbo 的图片须指定为首帧或尾帧")
			}
		} else if index == 0 {
			body["first_frame_url"] = url
		} else {
			body["last_frame_url"] = url
		}
	}
	if first != "" && body["first_frame_url"] == nil || last != "" && body["last_frame_url"] == nil {
		return nil, fmt.Errorf("已选择的首尾帧未包含在视频请求中")
	}
	if len(input.ReferenceImages) == 0 {
		body["aspect_ratio"] = defaultString(strings.TrimSpace(input.Config.Size), "16:9")
	}
	if len(input.ReferenceAudios) > 0 {
		if duration < 2 {
			return nil, fmt.Errorf("使用配音时视频须至少 2 秒")
		}
		url, err := BeefAPIAudioURL(input.ReferenceAudios[0])
		if err != nil {
			return nil, err
		}
		body["target_audio_url"] = url
	}
	return body, nil
}
