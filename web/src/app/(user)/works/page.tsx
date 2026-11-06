"use client";

import Link from "next/link";
import { useState } from "react";
import { App, Button, Card, Empty, Image, Input, Modal, Pagination, Segmented, Space, Spin, Tag, Typography } from "antd";
import { Download, ImageIcon, KeyRound, RefreshCw, Trash2, Video } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { saveAs } from "file-saver";

import { useUserStore } from "@/stores/use-user-store";
import { useCopyText } from "@/hooks/use-copy-text";
import { formatBytes } from "@/lib/image-utils";
import { downloadMediaWork, fetchMediaWorks, removeMediaWork, type MediaWork } from "@/services/api/works";

export default function WorksPage() {
    const { message, modal } = App.useApp();
    const token = useUserStore((state) => state.token);
    const userId = useUserStore((state) => state.user?.id);
    const copyText = useCopyText();
    const [keyword, setKeyword] = useState("");
    const [type, setType] = useState("all");
    const [page, setPage] = useState(1);
    const [selected, setSelected] = useState<MediaWork | null>(null);
    const [downloading, setDownloading] = useState("");
    const query = useQuery({
        queryKey: ["account-works", userId, keyword, type, page],
        queryFn: () => fetchMediaWorks(token!, { keyword, type, page, pageSize: 24 }),
        enabled: Boolean(token && userId),
        refetchInterval: (query) => query.state.data?.items.some((item) => item.kind === "image" && item.thumbnailState === "pending") ? 15000 : false,
    });
    const download = async (work: MediaWork) => {
        if (!token) return;
        setDownloading(work.id);
        try {
            const blob = await downloadMediaWork(token, work);
            const extension = work.mimeType === "image/jpeg" ? "jpg" : work.mimeType.split("/")[1]?.split(";")[0] || "bin";
            saveAs(blob, `Kai-${work.id}.${extension}`);
        } catch (error) { message.error(error instanceof Error ? error.message : "下载失败"); }
        finally { setDownloading(""); }
    };
    const remove = (work: MediaWork) => modal.confirm({
        title: "从作品库移除？", content: "这里只移除作品列表记录，不删除其他画布、素材或历史中仍在使用的原文件。",
        okText: "移除", cancelText: "取消",
        onOk: async () => { await removeMediaWork(token!, work.id); setSelected(null); await query.refetch(); },
    });
    return (
        <main className="h-full overflow-y-auto bg-background px-6 py-8 text-[var(--ant-color-text)]">
            <div className="mx-auto max-w-7xl">
                <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
                    <div>
                        <h1 className="text-3xl font-semibold">我的作品</h1>
                        <Typography.Paragraph type="secondary" className="!mb-0 !mt-2">
                            登录后生成并归档的图片自动汇集于此，也包含已存入云端的视频。我的素材仍用于手动收藏。
                        </Typography.Paragraph>
                        <Typography.Text type="secondary">列表加载小尺寸封面，查看和下载使用原文件；GIF 封面为静态图，原文件保留动画。</Typography.Text>
                    </div>
                    {token ? <Space wrap>
                        <Link href="/image-api"><Button icon={<KeyRound className="size-4" />}>AI 生图接口</Button></Link>
                        <Button icon={<RefreshCw className="size-4" />} loading={query.isFetching} onClick={() => void query.refetch()}>刷新作品</Button>
                    </Space> : null}
                </div>
                {!token ? <Empty description="请先登录，以查看自己的云端作品"><Link href="/login?redirect=%2Fworks"><Button type="primary">登录</Button></Link></Empty> : <>
                    <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
                        <Input.Search className="!max-w-md" placeholder="搜索提示词、标题或模型" allowClear onSearch={(value) => { setKeyword(value); setPage(1); }} />
                        <Segmented value={type} options={[{ label: "全部", value: "all" }, { label: "图片", value: "image" }, { label: "视频", value: "video" }]} onChange={(value) => { setType(value); setPage(1); }} />
                    </div>
                    {query.isLoading ? <div className="py-16 text-center"><Spin /></div> : query.isError ? <Empty description={query.error.message} /> : !query.data?.items.length ? <Empty description="还没有云端作品。生成的图片归档后会自动出现，已有内容可能需要稍等片刻后刷新。" /> : <>
                        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                            {query.data.items.map((work) => <Card key={work.id} styles={{ body: { padding: 14 } }} cover={
                                <button type="button" className="flex aspect-[4/3] w-full items-center justify-center overflow-hidden bg-[var(--ant-color-fill-quaternary)]" onClick={() => setSelected(work)} aria-label={`查看作品：${work.title}`}>
                                    {work.thumbnailUrl ? <img src={work.thumbnailUrl} alt={work.title} loading="lazy" decoding="async" className="h-full w-full object-cover" /> : <div className="flex flex-col items-center gap-2 text-[var(--ant-color-text-tertiary)]">{work.kind === "video" ? <Video className="size-10" /> : <ImageIcon className="size-10" />}<span className="text-xs">{work.kind === "video" ? "点击播放" : work.thumbnailState === "pending" ? "封面准备中" : "点击查看原图"}</span></div>}
                                </button>
                            }>
                                <div className="mb-2 line-clamp-2 min-h-10 text-sm font-medium">{work.title}</div>
                                <div className="mb-3 flex flex-wrap gap-1"><Tag>{work.kind === "video" ? "视频" : work.mimeType === "image/gif" ? "GIF" : "图片"}</Tag>{work.model ? <Tag>{work.model}</Tag> : null}</div>
                                <div className="mb-3 text-xs text-[var(--ant-color-text-tertiary)]">{new Date(work.createdAt).toLocaleString()} · {formatBytes(work.bytes)}</div>
                                <Space><Button size="small" icon={<Download className="size-3.5" />} loading={downloading === work.id} onClick={() => void download(work)}>原文件</Button><Button size="small" type="text" icon={<Trash2 className="size-3.5" />} aria-label="移除作品" onClick={() => remove(work)} /></Space>
                            </Card>)}
                        </div>
                        <div className="mt-6 flex justify-center"><Pagination current={page} pageSize={24} total={query.data.total} showSizeChanger={false} onChange={setPage} /></div>
                    </>}
                </>}
            </div>
            <Modal title={selected?.title} open={Boolean(selected)} width={960} onCancel={() => setSelected(null)} footer={selected ? <Space wrap>{selected.prompt ? <Button onClick={() => copyText(selected.prompt, "提示词已复制")}>复制提示词</Button> : null}<Button type="primary" loading={downloading === selected.id} onClick={() => void download(selected)}>下载原文件</Button></Space> : null} destroyOnHidden>
                {selected ? <div className="space-y-4">{selected.kind === "video" ? <video src={selected.url} controls preload="metadata" className="max-h-[65vh] w-full" /> : <Image src={selected.url} alt={selected.title} preview={false} className="!max-h-[65vh] !object-contain" />}<Typography.Paragraph className="whitespace-pre-wrap">{selected.prompt}</Typography.Paragraph><Typography.Text type="secondary">{selected.width && selected.height ? `${selected.width} × ${selected.height} · ` : ""}{selected.mimeType} · {formatBytes(selected.bytes)}</Typography.Text></div> : null}
            </Modal>
        </main>
    );
}
