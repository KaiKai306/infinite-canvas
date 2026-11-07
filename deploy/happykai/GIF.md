# GIF 生成与 New API 渠道配置

在生图工作台或画布图片设置中选择GPT Image 2.6图片模型，接口模式用 `images`，输出格式选择“GIF 动图”。可设置2至16帧、每帧20至2000毫秒，默认4帧/200毫秒。GIF请求自动关闭流式中间图；PNG保持既有行为。节点及工作台历史自动保存格式参数；跨设备默认值在顶部“配置→图片输出默认值”中设置，再点“完成”保存。工作台单次调整不会自动覆盖账号默认。旧PNG历史不会因当前GIF设置而改变重试格式。

结果继续使用服务端任务、私有R2、我的作品及静态缩略图。原始GIF不重编码，查看/下载读取完整动画。独立AI接口使用相同字段，详见 [IMAGE-API.md](IMAGE-API.md)。当前仅接原生OpenAI Images兼容路径，`responses`、`chat`及自定义参数转换模型选择GIF时会在请求前提示改用images；未新增Agent专属格式设置。

## 当前 New API 的操作

适用于本站 `v1.0.0-rc.47-hk002`，准确源码提交 `e4228a11d4cad8316e40ef740e530b146826358f`。普通JSON图片请求保留 `output_format`，但未知的 `frame_count`、`frame_duration_ms` 在标准转换时被移除。只需单渠道请求体透传即可保留完整参数，无须先改New API源码。

1. New API后台打开“渠道”，编辑对应的OpenAI官方渠道。
2. 找到“透传请求体 / Pass Through Body”。宽屏可在快捷开关中调整；完整入口在请求设置里的请求处理区域。
3. 只开启请求体透传，确认提示后保存；请求头透传与全局透传保持原设置，不需要跟着开启。
4. 确认该渠道包含要使用的真实模型名，例如 `gpt-image-2.6-flare`，Canvas个人渠道和New API令牌也允许这个模型。

该版本请求体透传会跳过模型重定向、参数覆盖及格式转换，不能同时依赖这些功能。如果一个现有渠道还服务其他需要映射的模型，建议另建专用OpenAI渠道，仅放所需2.6图片模型，沿用官方地址和本人官方凭据，再开透传。不要把官方Key粘贴到聊天或文档；Canvas继续使用New API生成的用户模型Key。

透传本身不会把PNG变成GIF，调用方仍须明确发送：

```json
{"model":"gpt-image-2.6-flare","prompt":"...","output_format":"gif","frame_count":4,"frame_duration_ms":200}
```

## 验收边界与以后升级

已用相同New API生产镜像、全新隔离数据库与本地假模型，验证关闭单渠道透传会丢失帧参数、开启后可保留；不使用生产账号、数据库或收费上游。此证据验证转发行为，不能替代本人官方账号的2.6 GIF权限和实际生成验收。

GPT Image 2.6 GIF字段对照Playground的请求实现和测试：

- https://github.com/CookSleep/gpt_image_playground/blob/main/src/lib/gptImage26.test.ts
- https://github.com/CookSleep/gpt_image_playground/blob/main/src/lib/openaiCompatibleImageApi.ts

官方文档索引尚未覆盖所查2.6 GIF契约，不能用旧的2.5格式列表断言2.6不支持，也不把开源测试当作官方API承诺。首次真实验收建议1个GIF、较小尺寸及较少帧，若返回模型/参数错误，保留具体错误，不自动重试收费生成。

以后若需要保留New API模型映射/参数覆盖，同时支持动画，应在定制版本显式增加这两个DTO字段及转发测试，再按正常备份、构建、升级流程实施；不建议无选择地恢复所有未知字段合并。上游原生支持以后可撤销此开关并在隔离环境重新验证。
