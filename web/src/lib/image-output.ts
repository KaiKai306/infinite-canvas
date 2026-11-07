import type { AiConfig } from "@/stores/use-config-store";

export type ImageOutputParameters = { output_format?: "gif"; frame_count?: number; frame_duration_ms?: number };
export type ImageOutputSettingKey = "imageOutputFormat" | "imageFrameCount" | "imageFrameDurationMs";
type ImageOutputConfig = Pick<AiConfig, "model" | "apiMode" | ImageOutputSettingKey>;

export function supportsGifOutput(model: string) {
    const match = model.trim().match(/^gpt-image-(\d+)(?:\.(\d+))?(?:-|$)/i);
    return Boolean(match && (Number(match[1]) > 2 || Number(match[1]) === 2 && Number(match[2] || 0) >= 6));
}

export function imageOutputParameters(config: ImageOutputConfig): ImageOutputParameters {
    if (!config.imageOutputFormat || config.imageOutputFormat === "png") return {};
    if (config.imageOutputFormat !== "gif") throw new Error("输出格式仅支持 PNG 或 GIF");
    if (!supportsGifOutput(config.model)) throw new Error("GIF 输出需要 GPT Image 2.6 或更新的图片模型");
    if (config.apiMode !== "images") throw new Error("GIF 请使用 images 接口模式");
    const frames = Number(config.imageFrameCount || 4);
    const duration = Number(config.imageFrameDurationMs || 200);
    if (!Number.isInteger(frames) || frames < 2 || frames > 16) throw new Error("GIF 帧数为2至16帧");
    if (!Number.isInteger(duration) || duration < 20 || duration > 2000) throw new Error("GIF 每帧时长为20至2000毫秒");
    return { output_format: "gif", frame_count: frames, frame_duration_ms: duration };
}

export function prepareImageOutputConfig(config: AiConfig): AiConfig {
    const output = imageOutputParameters(config);
    return output.output_format === "gif" ? { ...config, streamImages: "", responseFormatB64Json: "" } : config;
}
