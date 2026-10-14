import { InputNumber, Select } from "antd";
import { Switch } from "@/components/ui/base/switch";
import { isH3Style, type H3VideoOptions } from "@/lib/h3-video";

export function CanvasH3Controls({ model, value = {}, onChange, duration, onDurationChange }: { model: string; value?: H3VideoOptions; onChange: (value: H3VideoOptions) => void; duration?: string; onDurationChange?: (value: string) => void }) {
    const base = model.split("::").at(-1) === "minimax-h3";
    const style = isH3Style(model),
        vhs = model.split("::").at(-1) === "minimax-h3-max-style-vhs";
    const modes = [{ value: "balanced", label: "均衡" }, { value: "quality", label: "高质量" }, ...(base ? [{ value: "fast", label: "快速" }] : []), { value: "disabled", label: "关闭" }];
    return (
        <details className="rounded-lg border border-[var(--color-border)] px-3 py-2" data-canvas-no-zoom>
            <summary className="cursor-pointer text-[var(--fs-label)]">H3 高级设置</summary>
            <div className="mt-3 flex flex-col gap-3 text-[var(--fs-label)]">
                {!base && !style && onDurationChange ? (
                    <label className="flex min-w-0 items-center justify-between gap-3">
                        <span>时长（秒）</span>
                        <InputNumber
                            aria-label="H3 时长（秒）"
                            size="small"
                            min={0.92}
                            max={15}
                            step={0.01}
                            value={Number(duration || 5)}
                            onChange={(v) => {
                                if (v != null) onDurationChange(String(v));
                            }}
                        />
                    </label>
                ) : null}
                {!style ? (
                    <label className="flex min-w-0 items-center justify-between gap-3">
                        <span>提示词增强</span>
                        <Select aria-label="提示词增强" size="small" className="w-28" value={value.prompt_expansion_mode || "balanced"} options={modes} onChange={(mode) => onChange({ ...value, prompt_expansion_mode: mode })} />
                    </label>
                ) : null}
                <label className="flex min-w-0 items-center justify-between gap-3">
                    <span>随机种子</span>
                    <InputNumber
                        aria-label="随机种子"
                        size="small"
                        className="!w-36"
                        precision={0}
                        min={-Number.MAX_SAFE_INTEGER}
                        max={Number.MAX_SAFE_INTEGER}
                        value={value.seed}
                        placeholder="留空随机"
                        onChange={(seed) => onChange({ ...value, seed: seed == null ? undefined : seed })}
                    />
                </label>
                {vhs ? (
                    <label className="flex min-w-0 items-center justify-between gap-3">
                        <span>磁带磨损</span>
                        <Select
                            aria-label="磁带磨损"
                            size="small"
                            value={value.damage_level || "medium"}
                            options={[
                                { value: "light", label: "轻度" },
                                { value: "medium", label: "中度" },
                                { value: "heavy", label: "重度" },
                            ]}
                            onChange={(damage_level) => onChange({ ...value, damage_level })}
                        />
                    </label>
                ) : null}
                {!style ? (
                    <label className="flex min-w-0 items-center justify-between gap-3">
                        <span>内容安全检查</span>
                        <Switch aria-label="内容安全检查" checked={value.enable_safety_checker !== false} onChange={(enabled) => onChange({ ...value, enable_safety_checker: enabled })} />
                    </label>
                ) : null}
                {!style ? (
                    <label className="flex min-w-0 items-center justify-between gap-3">
                        <span>直接返回视频数据</span>
                        <Switch aria-label="直接返回视频数据" checked={value.sync_mode === true} onChange={(enabled) => onChange({ ...value, sync_mode: enabled })} />
                    </label>
                ) : null}
            </div>
        </details>
    );
}
