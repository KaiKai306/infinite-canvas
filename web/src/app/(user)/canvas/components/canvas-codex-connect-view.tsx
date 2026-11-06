"use client";

import { useEffect, useState } from "react";
import { Button, Input, Tooltip, Typography } from "antd";
import { Copy, KeyRound, Link2, MessageSquare, PlugZap } from "lucide-react";
import { useCopyText } from "@/hooks/use-copy-text";
import { canvasThemes } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";
import type { useCodexAgent } from "../agent/use-codex-agent";

const pluginCommand = "codex plugin marketplace add https://github.com/tigerowo/infinite-canvas.git\ncodex plugin add canvas-agent@infinite-canvas";
const startCommand = "npx -y @tigerowo/canvas-agent@0.1.3";
const mcpCommand = "codex mcp add infinite-canvas -- npx -y @tigerowo/canvas-agent@0.1.3 mcp";

export function CanvasCodexConnectView({ agent, onChat }: {
    agent: Pick<ReturnType<typeof useCodexAgent>, "connection" | "status" | "error" | "connect" | "disconnect">;
    onChat: () => void;
}) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const copyText = useCopyText();
    const [draft, setDraft] = useState(agent.connection);
    const connected = agent.status === "ready";
    const connecting = agent.status === "connecting";
    const statusText = { idle: "未连接", connecting: "连接中", ready: "已连接", error: "连接失败" }[agent.status];
    useEffect(() => setDraft(agent.connection), [agent.connection.endpoint, agent.connection.token]);
    const commandBlock = (command: string, label?: string) => (
        <div className="flex items-center gap-2 rounded-md border px-2.5 py-2" style={{ borderColor: theme.node.stroke }}>
            {label ? <span className="shrink-0 text-[11px]" style={{ color: theme.node.muted }}>{label}</span> : null}
            <code className="thin-scrollbar min-w-0 flex-1 overflow-x-auto whitespace-pre text-[11px] leading-5">{command}</code>
            <Tooltip title="复制命令"><Button type="text" size="small" className="!size-6 !min-w-6 shrink-0" style={{ color: theme.node.muted }} icon={<Copy className="size-3.5" />} onClick={() => copyText(command, "命令已复制")} aria-label={label ? `复制${label}命令` : "复制命令"} /></Tooltip>
        </div>
    );
    return (
        <div className="space-y-4">
            <div>
                <h2 className="text-base font-semibold leading-6">让本地 Codex 操作当前画布</h2>
                <p className="mt-1 text-xs leading-5" style={{ color: theme.node.muted }}>浏览器与本地 Agent 在同一台电脑运行，需要 Node.js 22+。本站部署在云端，连接服务运行在你的电脑，无须在服务器安装 Codex。</p>
            </div>
            <div className="space-y-2 px-3 py-2.5">
                <h3 className="text-sm font-medium">方式一：已有 Codex，直接添加画布工具</h3>
                <p className="text-xs leading-5" style={{ color: theme.node.muted }}>在本机终端注册 MCP（只需一次），然后新建 Codex 对话加载画布工具。已有同名插件或 MCP 时二选一，避免重复安装。</p>
                {commandBlock(mcpCommand, "注册 MCP")}
                <p className="text-xs leading-5" style={{ color: theme.node.muted }}>每次使用先启动下面的本地服务并保持终端运行，将 Local URL 和 Connect token 填入下方并连接。默认地址是 http://127.0.0.1:3210；浏览器询问本地网络权限时需允许。</p>
                {commandBlock(startCommand, "启动服务")}
                <p className="text-xs leading-5" style={{ color: theme.node.muted }}>保持当前画布网页打开，对本地 Codex 说：“通过 infinite-canvas 读取当前画布，再添加一个文字节点”。先确认读取和编辑正常，再让它触发生图。仅启动服务不会给外部 Codex 安装工具。</p>
            </div>
            <div className="space-y-2 rounded-lg border px-3 py-2.5" style={{ borderColor: theme.node.stroke }}>
                <div className="text-xs font-medium">账号与生图使用哪套配置</div>
                <p className="text-xs leading-5" style={{ color: theme.node.muted }}>Codex 的理解与规划使用本机 Codex 的模型配置；触发画布生图时使用网页当前账号及选中的图片渠道。建议先登录本站，结果才能按账号归档；操作画布时网页需保持打开，已提交的服务端图片任务不受关页影响。</p>
                <p className="text-xs leading-5" style={{ color: theme.node.muted }}>Connect token 只用于本机连接，不是 New API Key，不要分享。只连接目标画布，避免多个标签同时连接造成目标不明确。</p>
            </div>
            <div className="space-y-2 px-3 py-2.5">
                <h3 className="text-sm font-medium">方式二：安装上游插件自动连接</h3>
                <p className="text-xs leading-5" style={{ color: theme.node.muted }}>与方式一二选一。插件包含 MCP 和自动打开画布的指引；安装后新建 Codex 对话，明确提供本站或当前画布的完整地址。</p>
                {commandBlock(pluginCommand, "安装插件")}
                <p className="text-xs leading-5" style={{ color: theme.node.muted }}>例如：“帮我打开并连接 https://canvas.happykai.cn 的画布”。也可只启动本地服务，通过网页内的 Codex 对话操作，无须给外部 Codex 注册 MCP。</p>
            </div>
            <div className="rounded-lg border p-3" style={{ borderColor: theme.node.stroke }}>
                <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                            <span className="text-sm font-medium">网页连接</span>
                            <span className="inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px]" style={{ borderColor: theme.node.stroke, color: connected ? theme.node.text : theme.node.muted }}><span className="size-1.5 rounded-full bg-current" />{statusText}</span>
                        </div>
                        <p className="mt-1 text-xs leading-5" style={{ color: theme.node.muted }}>自动使用插件带入或已保存的连接信息，也可手动填写。</p>
                    </div>
                    <Button type={connected || connecting ? "default" : "primary"} icon={<PlugZap className="size-4" />} onClick={async () => {
                        if (connected || connecting) agent.disconnect();
                        else if (await agent.connect(draft)) onChat();
                    }}>{connected || connecting ? "断开" : "连接"}</Button>
                </div>
                <div className="mt-3 grid gap-3">
                    <label className="grid gap-1.5">
                        <span className="flex items-center gap-1.5 text-xs" style={{ color: theme.node.muted }}><Link2 className="size-3.5" />本地地址 <span className="opacity-70">Local URL</span></span>
                        <Input size="large" prefix={<Link2 className="mr-1 size-4" style={{ color: theme.node.faint }} />} aria-label="本地 Agent 地址" value={draft.endpoint} onChange={(event) => setDraft((current) => ({ ...current, endpoint: event.target.value }))} placeholder="http://127.0.0.1:3210" />
                    </label>
                    <label className="grid gap-1.5">
                        <span className="flex items-center gap-1.5 text-xs" style={{ color: theme.node.muted }}><KeyRound className="size-3.5" />连接 Token <span className="opacity-70">Connect token</span></span>
                        <Input.Password size="large" prefix={<KeyRound className="mr-1 size-4" style={{ color: theme.node.faint }} />} aria-label="连接 Token" value={draft.token} onChange={(event) => setDraft((current) => ({ ...current, token: event.target.value }))} placeholder="插件自动填入，或粘贴 Connect token" />
                    </label>
                    {agent.error ? <Typography.Text type="danger" role="alert" className="!text-xs leading-5">{agent.error}</Typography.Text> : null}
                    {connected ? <Button type="primary" icon={<MessageSquare className="size-4" />} onClick={onChat}>进入 Codex 对话</Button> : null}
                </div>
            </div>
        </div>
    );
}
