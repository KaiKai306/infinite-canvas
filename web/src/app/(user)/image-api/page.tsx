"use client";

import Link from "next/link";
import { useState } from "react";
import { Alert, App, Button, Card, Form, Input, InputNumber, Modal, Select, Space, Spin, Table, Tag, Typography } from "antd";
import { useQuery } from "@tanstack/react-query";
import { useUserStore } from "@/stores/use-user-store";
import { useConfigStore } from "@/stores/use-config-store";
import { useCopyText } from "@/hooks/use-copy-text";
import { createImageAPICredential, fetchImageAPIConfig, fetchImageAPICredentials, revokeImageAPICredential, type ImageAPICredential } from "@/services/api/image-api";

export default function ImageAPIPage() {
    const { message, modal } = App.useApp();
    const token = useUserStore((state) => state.token);
    const userId = useUserStore((state) => state.user?.id);
    const openConfig = useConfigStore((state) => state.openConfigDialog);
    const copyText = useCopyText();
    const [name, setName] = useState("我的 Codex");
    const [channelId, setChannelId] = useState("");
    const [model, setModel] = useState("");
    const [days, setDays] = useState(7);
    const [secret, setSecret] = useState("");
    const [creating, setCreating] = useState(false);
    const settings = useQuery({ queryKey: ["image-api-config", userId], queryFn: () => fetchImageAPIConfig(token!), enabled: Boolean(token && userId) });
    const credentials = useQuery({ queryKey: ["image-api-credentials", userId], queryFn: () => fetchImageAPICredentials(token!), enabled: Boolean(token && userId) });
    const selectedChannelId = channelId || settings.data?.defaultChannelId || "";
    const selectedModel = model || settings.data?.defaultModel || "";
    const create = async () => {
        if (!token) return;
        setCreating(true);
        try { const result = await createImageAPICredential(token, { name, channelId: selectedChannelId, model: selectedModel, days }); setSecret(result.secret); await credentials.refetch(); }
        catch (error) { message.error(error instanceof Error ? error.message : "创建失败"); }
        finally { setCreating(false); }
    };
    const revoke = (item: ImageAPICredential) => modal.confirm({ title: "撤销这个 AI 凭证？", content: "撤销后不能再提交、查询或下载；已经接受的任务仍会完成并保留在你的作品库。", okText: "撤销", cancelText: "取消", onOk: async () => { await revokeImageAPICredential(token!, item.id); await credentials.refetch(); } });
    const instructions = `生图服务：https://canvas.happykai.cn/api/image/v1
使用 Authorization: Bearer <你的专用生图凭证>，不要使用网页登录密码或 New API Key。
先 GET /capabilities 了解输入限制。
POST /jobs，必须带 Idempotency-Key（每次意图一个 UUID，网络重试复用原值）。
JSON 示例：{"prompt":"一只在窗边看雪的橘猫，水彩风格","size":"1024x1024","count":1}
GIF 示例：{"model":"gpt-image-2.6-flare","prompt":"循环飘落的雪花","output_format":"gif","frame_count":4,"frame_duration_ms":200}。使用本人渠道支持的2.6图片模型，2至16帧、每帧20至2000毫秒；服务端不发送流式参数。
参考图编辑使用 multipart/form-data：同名文本字段，加最多4个 image 文件（PNG/JPEG/WebP，每张不超过10MiB）。
GET /jobs/{id} 查询，每3秒一次，直到 completed 或 failed。
completed 后使用相同凭证下载 images[].downloadUrl 指定的相对路径。
相同任务号参数不一致返回409；不要对 failed 或 submission_unknown 自动更换任务号再次生成。
生成结果归入当前用户的“我的作品”，不需要打开画布或运行本地 Canvas Agent。`;
    return <main className="h-full overflow-y-auto bg-background px-6 py-8 text-[var(--ant-color-text)]"><div className="mx-auto max-w-5xl space-y-5">
        <div><h1 className="text-3xl font-semibold">AI 生图接口</h1><Typography.Paragraph type="secondary" className="!mt-3">授权自己的 AI 直接生成和下载图片，使用本人保存的个人渠道，结果自动归档到作品库。</Typography.Paragraph></div>
        {!token ? <Card><Typography.Paragraph>请先登录，再创建只属于你的生图凭证。</Typography.Paragraph><Link href="/login?redirect=%2Fimage-api"><Button type="primary">登录</Button></Link></Card> : <>
            <Alert type="info" title="生图凭证仅能调用图片任务、状态和下载接口" description="它不开放管理后台、账号设置或其他用户作品。原始凭证只在创建时显示一次，可随时撤销；最长有效30天。New API 模型 Key 由你在个人渠道中保存，此处不会自动创建。" />
            <Card title="创建专用凭证" extra={<Space><Button onClick={() => openConfig(false)}>个人渠道配置</Button><Button loading={settings.isFetching} onClick={() => void settings.refetch()}>刷新配置</Button></Space>}>
                {settings.isLoading ? <Spin /> : <>
                    {settings.isError || !settings.data?.channels.length ? <Alert type="warning" title="请先配置并保存个人渠道的地址、API Key 和默认生图模型，再刷新配置。" /> : null}
                    {settings.data && !settings.data.archiveEnabled ? <Alert type="warning" title="站点尚未启用服务端图片归档，暂不能创建生图凭证。" /> : null}
                    <Form layout="vertical" className="!mt-4"><div className="grid gap-4 md:grid-cols-2">
                        <Form.Item label="名称"><Input value={name} maxLength={64} onChange={(event) => setName(event.target.value)} /></Form.Item>
                        <Form.Item label="个人渠道"><Select className="w-full" value={selectedChannelId || undefined} options={settings.data?.channels.map((channel) => ({ label: channel.name || channel.id, value: channel.id }))} onChange={(value) => { setChannelId(value); setModel(""); }} /></Form.Item>
                        <Form.Item label="默认生图模型" extra="应填写这个个人渠道实际支持的图片模型；请求可选择同渠道内的其他模型。"><Input value={selectedModel} onChange={(event) => setModel(event.target.value)} /></Form.Item>
                        <Form.Item label="有效天数"><InputNumber min={1} max={30} value={days} onChange={(value) => setDays(value || 7)} /></Form.Item>
                    </div><Button type="primary" loading={creating} disabled={!settings.data?.archiveEnabled || !selectedChannelId || !selectedModel.trim()} onClick={() => void create()}>生成 AI 凭证</Button></Form>
                </>}
            </Card>
            <Card title="我的凭证"><Table<ImageAPICredential> rowKey="id" loading={credentials.isLoading} dataSource={credentials.data || []} pagination={false} scroll={{ x: 650 }} columns={[
                { title: "名称", dataIndex: "name" }, { title: "标识", dataIndex: "prefix" }, { title: "模型", dataIndex: "model" },
                { title: "到期时间", dataIndex: "expiresAt", render: (value: string) => new Date(value).toLocaleString() },
                { title: "状态", render: (_, item) => <Tag>{item.revokedAt ? "已撤销" : Date.parse(item.expiresAt) <= Date.now() ? "已过期" : "有效"}</Tag> },
                { title: "操作", render: (_, item) => <Button size="small" danger disabled={Boolean(item.revokedAt)} onClick={() => revoke(item)}>撤销</Button> },
            ]} /></Card>
            <Card title="交给 AI 的调用说明" extra={<Button onClick={() => copyText(instructions, "调用说明已复制")}>复制说明</Button>}><Typography.Paragraph className="whitespace-pre-wrap font-mono text-xs leading-6">{instructions}</Typography.Paragraph><Link href="/works">查看我的作品</Link></Card>
        </>}
        <Modal title="请保存你的专用生图凭证" open={Boolean(secret)} onCancel={() => setSecret("")} footer={<Space><Button onClick={() => copyText(secret, "凭证已复制")}>复制凭证</Button><Button type="primary" onClick={() => setSecret("")}>已保存，关闭</Button></Space>} destroyOnHidden><Typography.Paragraph>完整值只在此处显示一次。保存在本机密钥配置中，不要写进 Git 或公开文档。</Typography.Paragraph><Input.TextArea value={secret} readOnly autoSize={{ minRows: 2 }} /></Modal>
    </div></main>;
}
