package modelcatalog

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/beefapi"
	"infinite-canvas/backend/internal/kernel"
	"infinite-canvas/backend/internal/model"
)

var opaqueVideoAssetPattern = regexp.MustCompile(`^asset://[A-Za-z0-9_-]+$`)
var dreaminaSeedancePattern = regexp.MustCompile(`^dreamina-seedance-2[.-](0|5)(-|$)`)

func isNativeDreaminaSeedance(protocol, modelName string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(modelName)), "/")
	return model.IsVolcengineArkVideoProtocol(model.ChannelInterfaceType(protocol)) && dreaminaSeedancePattern.MatchString(parts[len(parts)-1])
}

func referenceDurationIsOpaqueAsset(media MediaRef) bool {
	return media.DurationMs == 0 && !strings.HasPrefix(media.StorageKey, "resource:") && opaqueVideoAssetPattern.MatchString(strings.TrimSpace(media.URL))
}

const (
	OfficialSeedanceImageMinEdge   = 300
	OfficialSeedanceImageMaxEdge   = 6000
	OfficialSeedanceImageMinAspect = 0.4
	OfficialSeedanceImageMaxAspect = 2.5
	OfficialSeedanceVideoMinPixels = 409600
	OfficialSeedanceVideoMaxPixels = 8295044
	// 火山方舟与腾讯 WAND 的 Seedance 文档都把参考音频单段下限写成 2 秒。
	OfficialSeedanceMinAudioSeconds = 2
)

const (
	// 火山方舟与腾讯 WAND 的输出时长下限都是 4 秒（2.0 为 4~15，2.5 为 4~30）。
	documentedSeedanceOutputMinSeconds = 4
	// 腾讯 WAND 1.0-pro（即 seedance-2.0）写明混合输入的总上限是 12 个文件；
	// 1.5-pro 和火山方舟都没有这条，所以只对我们自己的企业网关线路、且非 2.5 的模型收紧。
	documentedTencentReferenceFiles = 12
)

const (
	officialSeedanceImageMinEdge    = OfficialSeedanceImageMinEdge
	officialSeedanceImageMaxEdge    = OfficialSeedanceImageMaxEdge
	officialSeedanceImageMinAspect  = OfficialSeedanceImageMinAspect
	officialSeedanceImageMaxAspect  = OfficialSeedanceImageMaxAspect
	officialSeedanceVideoMinPixels  = OfficialSeedanceVideoMinPixels
	officialSeedanceVideoMaxPixels  = OfficialSeedanceVideoMaxPixels
	officialSeedanceMinAudioSeconds = OfficialSeedanceMinAudioSeconds
)

func isSeedance2Family(protocol, modelName string) bool {
	normalizedProtocol := strings.TrimSpace(protocol)
	if normalizedProtocol != "newapi-channel-2" && normalizedProtocol != "newapi" && normalizedProtocol != "openai" && normalizedProtocol != "openai-videos" && !model.IsVolcengineArkVideoProtocol(model.ChannelInterfaceType(normalizedProtocol)) {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(name, "seedance-2")
}

func isSeedance25Model(modelName string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(modelName)), "/")
	base := parts[len(parts)-1]
	return base == "seedance-2.5" || strings.HasPrefix(base, "seedance-2.5-") || strings.HasPrefix(base, "doubao-seedance-2-5") || strings.HasPrefix(base, "doubao-seedance-2.5") || base == "dreamina-seedance-2-5" || strings.HasPrefix(base, "dreamina-seedance-2-5-") || base == "dreamina-seedance-2.5" || strings.HasPrefix(base, "dreamina-seedance-2.5-")
}

const documentedSeedanceVideoMinPixels = 407696

func applySeedanceDocumentedVideoPixelFloor(config TaskConfig, refs *VideoReferenceConfig) {
	if refs == nil || refs.MinVideoPixels != officialSeedanceVideoMinPixels || refs.MaxVideoPixels != officialSeedanceVideoMaxPixels {
		return
	}
	if !isSeedance2Family(config.InterfaceType, config.Model) {
		return
	}
	if model.IsVolcengineArkVideoProtocol(model.ChannelInterfaceType(config.InterfaceType)) || seedanceMaterialLibraryHost(config.BaseURL) {
		refs.MinVideoPixels = documentedSeedanceVideoMinPixels
	}
}

func seedanceMaterialLibraryHost(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "beeftv.app", "enterprise.beefapi.com", "beefapi.com", "www.whatstoken.ai", "whatstoken.ai":
		return true
	default:
		return false
	}
}

// 桌面端直连企业网关时看域名；托管网页里请求走 /api/ai/system/beefapi，
// 同一渠道的 id 也是 beefapi，这里一并认作企业网关线路。
// WhatsToken 域名不在此列：它没有可核实的腾讯 WAND 映射，不跟着收紧。
func seedanceEnterpriseGatewayRoute(config TaskConfig) bool {
	if u, err := url.Parse(strings.TrimSpace(config.BaseURL)); err == nil {
		switch strings.ToLower(u.Hostname()) {
		case "beeftv.app", "enterprise.beefapi.com", "beefapi.com":
			return true
		}
	}
	if strings.EqualFold(strings.TrimSpace(config.ChannelID), beefapi.ChannelID) {
		return true
	}
	return SystemChannelIDFromBaseURL(config.BaseURL) == beefapi.ChannelID
}

// 腾讯 WAND 映射的是 BeefAPI 公开名 seedance-2.0（含 fast/mini 后缀）。
// 原生 Ark 名（doubao-seedance-2-0-*、dreamina-seedance-2-0-*）走的是字节官方合同，
// 即使挂在同一个网关域名下也没有腾讯「合计 12」这条限制，不扩到它们身上。
func tencentWandSeedance20Model(modelName string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(modelName)), "/")
	base := parts[len(parts)-1]
	return base == "seedance-2.0" || strings.HasPrefix(base, "seedance-2.0-")
}

func tencentWandReferenceRoute(config TaskConfig) bool {
	if !tencentWandSeedance20Model(config.Model) {
		return false
	}
	if model.IsVolcengineArkVideoProtocol(model.ChannelInterfaceType(config.InterfaceType)) {
		return false
	}
	return seedanceEnterpriseGatewayRoute(config)
}

// 输出时长下限官方是 4 秒：2.0 为 4~15，2.5 为 4~30。
// 区间、连续（fractionalMin）和枚举三种已保存配置都要收紧，枚举里的 -1（自动）保留。
func tightenSeedanceOutputDuration(duration *VideoDurationConfig) {
	minimum := documentedSeedanceOutputMinSeconds
	if duration == nil {
		return
	}
	if duration.Selection == "enum" {
		values := make([]int, 0, len(duration.Values))
		for _, seconds := range duration.Values {
			if seconds != -1 && seconds < minimum {
				continue
			}
			values = append(values, seconds)
		}
		if len(values) == 0 {
			return
		}
		duration.Values = values
		if duration.Default == -1 && containsInt(values, -1) || containsInt(values, duration.Default) {
			return
		}
		duration.Default = values[0]
		if duration.Default == -1 && len(values) > 1 {
			duration.Default = values[1]
		}
		return
	}
	if duration.Continuous && duration.FractionalMin > 0 && duration.FractionalMin < float64(minimum) {
		duration.FractionalMin = float64(minimum)
	}
	if duration.Min < minimum {
		duration.Min = minimum
	}
	if duration.Default < minimum {
		duration.Default = minimum
	}
}

// 官方文档写的下限只会收紧素材合同，不会放宽管理员已经配得更严的数值。
// 输出时长与参考音频下限对 Seedance 2.x 的所有线路都成立；「混合输入合计 12 个」
// 只在腾讯 WAND 1.0-pro（我们这边的 seedance-2.0）有，且只在我们自己的企业网关线路上。
func tightenSeedanceDocumentedRouteLimits(config TaskConfig, value *VideoCapabilityConfig) {
	if value == nil || !isSeedance2Family(config.InterfaceType, config.Model) {
		return
	}
	tightenSeedanceOutputDuration(&value.Duration)
	if value.References.MinAudioDuration < officialSeedanceMinAudioSeconds {
		value.References.MinAudioDuration = officialSeedanceMinAudioSeconds
	}
	if !tencentWandReferenceRoute(config) {
		return
	}
	if value.References.MaxTotalReferences <= 0 || value.References.MaxTotalReferences > documentedTencentReferenceFiles {
		value.References.MaxTotalReferences = documentedTencentReferenceFiles
	}
}

func tightenedVideoCapability(config TaskConfig, profile *VideoCapabilityConfig) *VideoCapabilityConfig {
	if profile == nil {
		return nil
	}
	value := *profile
	value.References = profile.References
	tightenSeedanceDocumentedRouteLimits(config, &value)
	return &value
}

func overlayOfficialSeedance2References(base VideoReferenceConfig, is25 bool) VideoReferenceConfig {
	refs := base
	if is25 {
		refs.MaxImages = 30
		refs.MaxVideos = 10
		refs.MaxAudios = 10
		refs.MaxVideoDuration = 30
		refs.MaxAudioDuration = 30
		refs.MaxAudioTotalDuration = 30
		refs.MaxVideoTotalDuration = 30
	} else {
		refs.MaxImages = 9
		refs.MaxVideos = 3
		refs.MaxAudios = 3
		refs.MaxVideoDuration = 15
		refs.MaxAudioDuration = 15
		refs.MaxAudioTotalDuration = 15
		refs.MaxVideoTotalDuration = 15
	}
	refs.MinVideoDuration = 2
	refs.MinAudioDuration = officialSeedanceMinAudioSeconds
	if refs.MaxImageBytes <= 0 {
		refs.MaxImageBytes = 30 * 1024 * 1024
	}
	refs.MaxVideoBytes = 200 * 1024 * 1024
	refs.MaxAudioBytes = 15 * 1024 * 1024
	refs.MinImageWidth, refs.MaxImageWidth = officialSeedanceImageMinEdge, officialSeedanceImageMaxEdge
	refs.MinImageHeight, refs.MaxImageHeight = officialSeedanceImageMinEdge, officialSeedanceImageMaxEdge
	refs.MinImageAspect, refs.MaxImageAspect = officialSeedanceImageMinAspect, officialSeedanceImageMaxAspect
	refs.MinVideoWidth, refs.MaxVideoWidth = officialSeedanceImageMinEdge, officialSeedanceImageMaxEdge
	refs.MinVideoHeight, refs.MaxVideoHeight = officialSeedanceImageMinEdge, officialSeedanceImageMaxEdge
	refs.MinVideoAspect, refs.MaxVideoAspect = officialSeedanceImageMinAspect, officialSeedanceImageMaxAspect
	refs.MinVideoPixels, refs.MaxVideoPixels = officialSeedanceVideoMinPixels, officialSeedanceVideoMaxPixels
	return refs
}

func validateVideoReferenceMedia(profile *VideoCapabilityConfig, input TaskInput) error {
	if profile == nil {
		return kernel.BadAuthRequest("当前视频模型能力参数无效")
	}
	profile = tightenedVideoCapability(input.Config, profile)
	refs := profile.References
	applySeedanceDocumentedVideoPixelFloor(input.Config, &refs)
	if isNativeDreaminaSeedance(input.Config.InterfaceType, input.Config.Model) && isSeedance25Model(input.Config.Model) && metadataString(input.Metadata, "videoEditOperation") == "inpaint" && refs.MinVideoDuration < 4 {
		refs.MinVideoDuration = 4
	}

	if len(input.ReferenceImages) > refs.MaxImages {
		return kernel.BadAuthRequest(fmt.Sprintf("当前视频模型最多支持 %d 张参考图", refs.MaxImages))
	}
	if len(input.ReferenceVideos) > refs.MaxVideos {
		return kernel.BadAuthRequest(fmt.Sprintf("当前视频模型最多支持 %d 个参考视频", refs.MaxVideos))
	}
	if len(input.ReferenceAudios) > refs.MaxAudios {
		return kernel.BadAuthRequest(fmt.Sprintf("当前视频模型最多支持 %d 段参考音频", refs.MaxAudios))
	}
	if total := len(input.ReferenceImages) + len(input.ReferenceVideos) + len(input.ReferenceAudios); refs.MaxTotalReferences > 0 && total > refs.MaxTotalReferences {
		return kernel.BadAuthRequest(fmt.Sprintf("当前视频模型最多支持 %d 个参考素材（图片、视频、音频合计），现在有 %d 个；请移除多余素材后再提交", refs.MaxTotalReferences, total))
	}
	if !videoCapabilityAllowsAudioOnly(profile) && len(input.ReferenceAudios) > 0 && len(input.ReferenceImages) == 0 && len(input.ReferenceVideos) == 0 {
		return kernel.BadAuthRequest("当前视频模型不支持只用音频生成视频，请同时添加参考图片或参考视频")
	}
	if len(input.ReferenceImages) < refs.MinImages {
		return kernel.BadAuthRequest(fmt.Sprintf("当前视频模型至少需要 %d 张参考图", refs.MinImages))
	}
	if input.DeferReferenceMetadata {
		return nil
	}
	for index, media := range input.ReferenceImages {
		if err := validateVideoReferenceImage(refs, index, media); err != nil {
			return err
		}
	}
	var totalVideoMs int64
	for index, media := range input.ReferenceVideos {
		totalVideoMs += media.DurationMs
		if err := validateVideoReferenceVideo(refs, index, media); err != nil {
			return err
		}
	}
	if maximum := refs.MaxVideoTotalDuration; maximum > 0 && totalVideoMs > int64(maximum)*1000 {
		return kernel.BadAuthRequest(fmt.Sprintf("参考视频总时长为 %.2f 秒，当前模型最多支持 %d 秒；请裁剪或减少参考视频后再提交", float64(totalVideoMs)/1000, maximum))
	}
	var totalAudioMs int64
	for index, media := range input.ReferenceAudios {
		if err := validateReferenceDurationUnlessOpaqueAsset("音频", index, media, refs.MinAudioDuration, float64(refs.MaxAudioDuration)); err != nil {
			return err
		}
		if err := validateReferenceFileBytes("音频", index, media.Bytes, refs.MaxAudioBytes); err != nil {
			return err
		}
		totalAudioMs += media.DurationMs
	}
	if maximum := refs.MaxAudioTotalDuration; maximum > 0 && totalAudioMs > int64(maximum)*1000 {
		return kernel.BadAuthRequest(fmt.Sprintf("参考音频总时长为 %.2f 秒，当前模型最多支持 %d 秒；请裁剪或减少参考音频后再提交", float64(totalAudioMs)/1000, maximum))
	}
	return nil
}

func validateVideoReferenceImage(refs VideoReferenceConfig, index int, media MediaRef) error {
	if err := validateReferenceFileBytes("图", index, media.Bytes, refs.MaxImageBytes); err != nil {
		return err
	}
	return validateReferenceGeometry("图", index, media.Width, media.Height, refs.MinImageWidth, refs.MaxImageWidth, refs.MinImageHeight, refs.MaxImageHeight, refs.MinImageAspect, refs.MaxImageAspect, refs.MinImagePixels, refs.MaxImagePixels)
}

func validateVideoReferenceVideo(refs VideoReferenceConfig, index int, media MediaRef) error {
	if err := validateReferenceDurationUnlessOpaqueAsset("视频", index, media, float64(refs.MinVideoDuration), float64(refs.MaxVideoDuration)); err != nil {
		return err
	}
	if err := validateReferenceFileBytes("视频", index, media.Bytes, refs.MaxVideoBytes); err != nil {
		return err
	}
	if refs.MinVideoPixels > 0 && (media.Width <= 0 || media.Height <= 0) && !opaqueVideoAssetPattern.MatchString(strings.TrimSpace(media.URL)) {
		return kernel.BadAuthRequest(fmt.Sprintf("第 %d 个参考视频尺寸无法读取，请重新导入素材后再提交", index+1))
	}
	return validateReferenceGeometry("视频", index, media.Width, media.Height, refs.MinVideoWidth, refs.MaxVideoWidth, refs.MinVideoHeight, refs.MaxVideoHeight, refs.MinVideoAspect, refs.MaxVideoAspect, refs.MinVideoPixels, refs.MaxVideoPixels)
}

func validateReferenceFileBytes(kind string, index int, bytes, maximum int64) error {
	if maximum <= 0 || bytes <= 0 || bytes <= maximum {
		return nil
	}
	unit := "张"
	if kind != "图" {
		unit = "个"
		if kind == "音频" {
			unit = "段"
		}
	}
	return kernel.BadAuthRequest(fmt.Sprintf("第 %d %s参考%s文件过大，当前模型单文件上限为 %s；请压缩或更换后再提交", index+1, unit, kind, formatMediaByteLimit(maximum)))
}

func validateReferenceDurationUnlessOpaqueAsset(kind string, index int, media MediaRef, minimum, maximum float64) error {
	if referenceDurationIsOpaqueAsset(media) {
		return nil
	}
	return validateReferenceDuration(kind, index, media.DurationMs, minimum, maximum)
}

func validateReferenceGeometry(kind string, index, width, height, minWidth, maxWidth, minHeight, maxHeight int, minAspect, maxAspect float64, minPixels, maxPixels int64) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	unit := "张"
	if kind != "图" {
		unit = "个"
	}
	label := fmt.Sprintf("第 %d %s参考%s", index+1, unit, kind)
	if minWidth > 0 && width < minWidth || maxWidth > 0 && width > maxWidth {
		return kernel.BadAuthRequest(fmt.Sprintf("%s宽度为 %d 像素，需要 %s 像素；请调整尺寸或更换后再提交", label, width, referenceBound(float64(minWidth), float64(maxWidth))))
	}
	if minHeight > 0 && height < minHeight || maxHeight > 0 && height > maxHeight {
		return kernel.BadAuthRequest(fmt.Sprintf("%s高度为 %d 像素，需要 %s 像素；请调整尺寸或更换后再提交", label, height, referenceBound(float64(minHeight), float64(maxHeight))))
	}
	aspect := float64(width) / float64(height)
	if minAspect > 0 && aspect < minAspect || maxAspect > 0 && aspect > maxAspect {
		return kernel.BadAuthRequest(fmt.Sprintf("%s宽高比为 %.2f，需要 %s；请调整尺寸或更换后再提交", label, aspect, referenceBound(minAspect, maxAspect)))
	}
	pixels := int64(width) * int64(height)
	if minPixels > 0 && pixels < minPixels || maxPixels > 0 && pixels > maxPixels {
		return kernel.BadAuthRequest(fmt.Sprintf("%s像素总量为 %d（%d×%d），需要 %s 像素；请调整这份素材的尺寸或更换原文件，修改生成分辨率不会改变参考素材", label, pixels, width, height, referenceBound(float64(minPixels), float64(maxPixels))))
	}
	return nil
}

func referenceBound(min, max float64) string {
	if min <= 0 {
		return "不超过 " + formatDurationBound(max)
	}
	if max <= 0 {
		return "至少 " + formatDurationBound(min)
	}
	return formatDurationBound(min) + "–" + formatDurationBound(max)
}

func formatMediaByteLimit(bytes int64) string {
	if bytes%(1024*1024) == 0 {
		return fmt.Sprintf("%dMB", bytes/(1024*1024))
	}
	return fmt.Sprintf("%d 字节", bytes)
}

func formatDurationBound(value float64) string {
	if value == math.Trunc(value) {
		return strconv.Itoa(int(value))
	}
	return strconv.FormatFloat(value, 'f', 1, 64)
}

func durationLimitMs(seconds float64) int64 {
	return int64(math.Round(seconds * 1000))
}

func videoCapabilityAllowsAudioOnly(profile *VideoCapabilityConfig) bool {
	if profile == nil {
		return false
	}
	return containsCapabilityString(profile.Operations, "audio_to_video")
}

func ValidateVideoReferenceMedia(profile *VideoCapabilityConfig, input TaskInput) error {
	return validateVideoReferenceMedia(profile, input)
}

func IsSeedance2Family(protocol, modelName string) bool {
	return isSeedance2Family(protocol, modelName)
}

func IsSeedance25Model(modelName string) bool {
	return isSeedance25Model(modelName)
}

func OverlayOfficialSeedance2References(base VideoReferenceConfig, is25 bool) VideoReferenceConfig {
	return overlayOfficialSeedance2References(base, is25)
}

func ApplySeedanceDocumentedVideoPixelFloor(config TaskConfig, refs *VideoReferenceConfig) {
	applySeedanceDocumentedVideoPixelFloor(config, refs)
}

func ValidateVideoReferenceImage(refs VideoReferenceConfig, index int, media MediaRef) error {
	return validateVideoReferenceImage(refs, index, media)
}

func ValidateVideoReferenceVideo(refs VideoReferenceConfig, index int, media MediaRef) error {
	return validateVideoReferenceVideo(refs, index, media)
}

func VideoCapabilityAllowsAudioOnly(profile *VideoCapabilityConfig) bool {
	return videoCapabilityAllowsAudioOnly(profile)
}
