# 媒体组件的许可与源码交付

这份文档说明发布流程如何交付 FFmpeg 相关组件的许可、对应源码和构建材料。它不重新许可第三方代码，也不把工程检查结果作为组合许可兼容性的法律结论。

## 分发内容

| 分发内容 | 媒体组件 | 源码交付方式 |
| --- | --- | --- |
| 桌面版和 Web 工作台 | `@ffmpeg/core` 0.12.10，GPL-2.0-or-later | 固定源码包与实际 JS/WASM 哈希对应；桌面 Release 与 COS 同时提供，Web 镜像内提供 |
| Windows 桌面版 | Gyan FFmpeg/ffprobe 8.0.1 essentials，GPL-3.0-or-later | 包含适用源码、补丁、依赖和构建脚本的源码包 |
| macOS 桌面版 | 固定 FFmpeg 源码构建的精简 ffprobe，LGPL-2.1-or-later | 每次编译同时归档原始源码、实际构建脚本、参数和二进制哈希 |
| Windows 可选深度运行包 | 独立 FFmpeg/ffprobe，以及 imageio-ffmpeg wheel 内的另一个 FFmpeg | 两套二进制分别审计；主 FFmpeg 的源码不能覆盖 wheel 中的程序 |

BeefTV 的商业使用限制不替代这些组件的许可证。GPL 组件能否与 BeefTV 新增源码可用内容组合分发，仍须根据实际调用、链接和分发方式审查。Worker、独立进程或单独下载本身都不是自动兼容证明。

## 源码输入与发布检查

`licenses/distribution-inputs.json` 记录预构建组件的二进制哈希和经过审查的源码包 URL、大小、SHA-256。`source: null` 表示缺少合格输入，发布必须停止，不能填入仅有主项目代码的归档来绕过检查。审查还应核对传递依赖、修改、构建脚本及许可证；自动校验只能验证已声明材料的完整性和对应关系。

```sh
python3 scripts/collect-distribution-sources.py --version "$(cat VERSION)" \
  --component ffmpeg-wasm --component ffmpeg-windows-amd64
```

源码包是带根 `manifest.json` 的 tar.gz，至少包含组件 `id`、`version`、`license`、`binaryHashes`，逐项列出路径与 SHA-256 的 `sources`，指向实际构建说明的 `buildInstructions`，以及指向完整许可文本的 `licenses`。`provenance.status` 必须是 `built` 或 `upstream-attested`，保留真实构建记录或上游证明；不能手改状态来标记未完成的审查。

macOS 脚本的第四个参数指定源码包输出。不要从旧二进制反推“本次构建”证明：

```sh
bash scripts/package-media-runtime-macos.sh darwin/arm64 ./media-runtime \
  ./ffmpeg-source.tar.gz ./BeefTV-vX.Y.Z-ffprobe-darwin-arm64-sources.tar.gz
```

最终桌面 ZIP 中必须有 `agent-host/licenses/`（macOS 位于应用 Resources 中）。检查脚本核对许可文本是否存在，以及最终程序中嵌入的 JS/WASM、最终原生工具和源码包的哈希：

```sh
python3 scripts/distribution_sources.py \
  --archive dist/BeefTV-vX.Y.Z-windows-amd64.zip --sources-dir dist \
  --platform windows-amd64 --version vX.Y.Z
```

正式桌面流水线强制使用发布器的 `--with-sources`：本地检查先于存储写探针，源码上传和公开读回先于桌面程序上传；激活更新时再核对 GitHub 源码资产的大小、摘要和 COS 字节。历史版本恢复命令的可选开关用于旧包兼容，不可用于绕过新版本的源码交付。源码与版本资产长期保留；Actions 短期产物不是用户下载入口。

正式 Web 镜像流水线先把源码收集到 `licenses/source-packets/`，再以 `VERIFY_SOURCE_DELIVERY=1` 构建。最终静态文件和源码包必须匹配才可推送。容器通过 `/licenses/` 提供许可，`/licenses/sources/` 提供对应源码。普通本地构建允许尚未收集的源码输入，不能据此认定可公开分发。

深度运行包在签名前运行 `python3 scripts/verify-depth-sources.py dist/depth --version "$(cat VERSION)"`。检查会复用已有的分片大小与哈希协议，临时重组 CUDA ZIP，逐一核对 CPU/CUDA 包中的主 FFmpeg、ffprobe 和 imageio 自带 FFmpeg，再删除临时重组文件。缺少许可、源码或出现未声明的 FFmpeg 都会停止发布。wheel 的 BSD 许可证不代表其内嵌 FFmpeg 的源码义务已经满足。

## 重建 WASM

`licenses/ffmpeg-wasm-sources.json` 固定官方 core 0.12.10 发布提交及其依赖候选源码。`scripts/package-wasm-sources.py` 收集这些归档、许可和离线构建配方。公开 npm 包缺少足以绑定历史构建输入的证明时，候选源码包保持 `unresolved`，`--require-resolved` 必须拒绝它。

```sh
python3 scripts/package-wasm-sources.py --version vX.Y.Z --output .local/wasm-sources
```

源码包中的 `BUILD.md` 说明如何准备工具链并断网编译。重建成功后应绑定**新**二进制与实际输入，并验收 H.264 剪辑、裁剪、合并、字幕和时间线导出，再修改应用采用的核心；不能把候选重建的证明贴到旧 npm 二进制上。保留现有编码能力，删除 GPL 编码器不是无行为变化的许可修复。

## 参考

- [FFmpeg 许可说明](https://ffmpeg.org/legal.html)
- [ffmpeg.wasm 官方许可问答](https://ffmpegwasm.netlify.app/docs/faq/)
- [GPLv3 许可证](https://opensource.org/license/gpl-3.0)
- [Gyan Windows 构建说明及联系入口](https://www.gyan.dev/ffmpeg/builds/)
