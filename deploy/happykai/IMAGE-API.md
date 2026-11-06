# 个人生图接口与作品库

登录本站，在账户菜单打开“AI 生图接口”。先保存本人个人渠道的地址、模型 API Key 和默认生图模型，再创建专用凭证。凭证绑定一个个人渠道，默认7天，最多30天；最多10个有效凭证，可单独撤销。完整值仅创建时返回，数据库只保存SHA-256。它不等同于New API Key或网页登录JWT，没有管理、配置修改、任意文件或其他用户数据权限。

API凭证代表登录用户，不需要浏览器持续登录或本地Canvas Agent。用户被本站禁用、凭证过期或撤销后拒绝新请求；已接受任务继续完成。模型调用由New API或对应渠道执行，使用该用户已保存的渠道Key；本站不会自动获取New API模型Key。外部New API账号状态仍以现有渠道鉴权为准，不新增每次跨机登录校验。

## 请求协议

基地址：`https://canvas.happykai.cn/api/image/v1`。仅接受请求头 `Authorization: Bearer <专用生图凭证>`，不接受Cookie或URL中的密钥。普通业务响应沿用 `{code,data,msg}`，`code=0`成功；下载返回原始二进制。所有响应禁止共享缓存。

| 路径 | 用途 |
| --- | --- |
| `GET /capabilities` | 读取绑定模型、渠道ID、输入限制和调用说明，不含渠道密钥 |
| `POST /jobs` | 提交图片任务，首次接受202；相同意图重试200 |
| `GET /jobs?page=1&pageSize=20` | 分页列出本人通过此接口创建的任务 |
| `GET /jobs/{id}` | 查询本人任务，返回状态和归档完成后的下载路径 |
| `GET /jobs/{id}/images/{index}` | 下载本人任务的原文件，沿用实际格式与原始字节 |

JSON提交示例：

```json
{"prompt":"一只在窗边看雪的橘猫，水彩风格","size":"1024x1024","quality":"auto","count":1}
```

支持字段只有 `prompt`、`model`、`size`、`quality`、`count`。提示词1至12000字符，单次1至4张，JSON最大1MiB。模型默认使用凭证创建时选择的模型，也可指定绑定个人渠道允许的其他模型；不能指定其他用户、渠道、Base URL或API Key。模型支持哪些尺寸/质量以本人渠道为准，不自动改模型。

参考图编辑使用 `multipart/form-data`，相同文本字段，加最多4个重复 `image` 文件字段，按上传顺序使用。每张最大10MiB、3200万像素，允许真实PNG/JPEG/WebP，整个请求最大42MiB。不要手工填写boundary；不接受远程参考图URL、本机路径字符串或JSON Base64图。

## 重试与任务状态

提交必须带 `Idempotency-Key`：8至128位字母、数字、下划线或短横线。每次生成意图分配一个UUID，网络重试复用原值和原始参数/图片；重复请求不会再次调用模型。相同任务号的参数或参考图内容不同返回409；文件名和multipart boundary不影响指纹。

服务在模型请求前持久化任务意图。无法确认已提交时返回 `submission_unknown`，不会自动重放模型请求。重启时尚未保存模型结果的API任务标为结果未知；已保存的图片继续走现有后台归档重试。此机制避免自动重放，不承诺在不支持上游幂等的情况下实现跨故障exactly-once。

正常状态为 `queued → processing → completed | failed`，也可能 `cancelled`。`processing` 且 `phase=archiving` 表示生成结果已保存，正在归档R2；存储失败只重试存储，不再次生图。完成时 `images` 包含 `index`、`mimeType`、尺寸、字节数和 `downloadUrl`。路径是本站相对路径，仍需同一专用凭证下载。

同用户每分钟最多10个新API任务，同时最多3个进行中API任务，429返回 `Retry-After`。查询建议每3秒一次。不要对 `failed` 或 `submission_unknown` 自动换新任务号再次生成；先核对结果/扣费再由用户决定。凭证撤销不取消已经运行的模型请求。

## 给本地 AI 的最小 Python 示例

通过本机密钥配置将凭证注入 `KAI_CANVAS_IMAGE_KEY`；不要将真实值写入Git、脚本或聊天。任务号应由调用方持久保留，恢复提交时复用，不在每次网络重试时重新生成。

```python
import json, os, time, uuid
from urllib.request import Request, urlopen
from urllib.parse import urljoin

origin = "https://canvas.happykai.cn"
base = origin + "/api/image/v1"
token = os.environ["KAI_CANVAS_IMAGE_KEY"]
intent = str(uuid.uuid4())  # 网络重试必须复用这个值及原始输入。

def call(path, body=None):
    headers = {"Authorization": "Bearer " + token}
    if body is not None:
        headers.update({"Content-Type": "application/json", "Idempotency-Key": intent})
        body = json.dumps(body).encode()
    with urlopen(Request(base + path, data=body, headers=headers), timeout=60) as r:
        payload = json.load(r)
    if payload["code"] != 0:
        raise RuntimeError(payload["msg"])
    return payload["data"]

capabilities = call("/capabilities")
job = call("/jobs", {"prompt": "一只在窗边看雪的橘猫，水彩风格", "count": 1})
while job["status"] in ("queued", "processing", "running", "in_progress"):
    time.sleep(3)
    job = call("/jobs/" + job["id"])
if job["status"] != "completed":
    raise RuntimeError(job.get("error") or job["status"])
image = job["images"][0]
request = Request(urljoin(origin, image["downloadUrl"]), headers={"Authorization": "Bearer " + token})
with urlopen(request, timeout=120) as r:
    if not r.headers.get("Content-Type", "").startswith("image/"):
        raise RuntimeError("下载未返回图片，请检查接口错误")
    extension = {"image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}.get(image["mimeType"], ".png")
    with open("Kai-" + job["id"] + extension, "wb") as output:
        output.write(r.read())
```

## 作品与缩略图

`/works` 只向登录用户展示本人作品，搜索/分页由服务器处理。画布、生图工作台及API产生的已归档图片自动登记；API完成不依赖浏览器打开。旧的已归档图片任务由后台补索引；不存在或已删除的旧原文件不能恢复。已在云存储文件索引中的视频可展示与播放，尚未归档的远端临时视频不在保证范围内；本轮不新增视频生成接口或完整后台视频归档。

图片封面由服务器生成最长边512像素、JPEG质量80的静态缩略图，原图和缩略图都存私有R2，不写永久本地媒体目录。原始GIF保留所有字节，列表仅使用首帧静态封面，点开或下载才读取原文件。视频列表使用占位图，点击播放后才加载视频，避免为显示列表下载整段视频。

缩略图异步生成，封面暂不可用时显示占位，不自动把整张原图当列表封面。输入超过32MiB或3200万像素、解码不支持时保留原文件，封面使用占位。浏览器懒加载和分页进一步限制首屏请求；现有画布/WebP本地缓存保留。

作品记录仅引用原R2对象，不复制原图；图片任务完成后数据库保留索引，不保存缩略图Base64。删除画布或历史时，作品库仍引用的原文件会保留。“从作品库移除”只隐藏作品记录，不直接删除R2文件、画布或素材；后台补索引不会让已移除作品重新出现。真正删除无引用云文件需要另行统一生命周期处理，不能扫描桶后直接全删。

文件和缩略图均校验所属用户，匿名、其他用户及仅持图片API凭证者不能使用网页登录文件入口；API凭证仅能下载本人API任务明确返回的文件。页面手动收藏继续使用“我的素材”，与自动归集的作品库分开。
