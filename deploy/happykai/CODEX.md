# 本地 Codex 操作 Kai's 无限画布

浏览器和本地 Canvas Agent 在同一台电脑运行；画布网站在186服务器。Agent提供网页与Codex之间的连接，模型理解与规划使用本地Codex的配置；画布生图使用网页当前账号及选中的图片渠道。两者无需使用相同的Key。

## 已有 Codex：直接注册 MCP

需要Node.js22+和已可用的Codex。当前网页匹配Canvas Agent0.1.3，先固定版本，不自动升级本机工具，也不读取Codex凭据。本机已核对Node.js24.11.1和Codex CLI0.166.0满足基础命令要求；不等于已联调画布或执行生成。

1. 在电脑终端执行一次，注册画布工具；已有同名插件/MCP时二选一，避免重复注册。

   ```powershell
   codex mcp add infinite-canvas -- npx -y @tigerowo/canvas-agent@0.1.3 mcp
   ```

2. 每次使用时启动本地服务，保持终端运行：

   ```powershell
   npx -y @tigerowo/canvas-agent@0.1.3
   ```

3. 在同一台电脑的浏览器打开 https://canvas.happykai.cn ，登录后进入一个具体画布。打开助手，切换到“Codex”，进入“连接教程”。填写本地服务输出的 `Local URL`（默认 `http://127.0.0.1:3210`）和 `Connect token`，点击连接。浏览器若询问本地网络访问权限，需允许该站点访问本机服务。

4. 新建本地Codex对话，使其加载新注册的MCP。先说：“使用 infinite-canvas 读取当前画布，然后添加一个写着‘连接测试’的文字节点”。先验证读取和编辑，再请求需要收费的图片生成。多个已连接画布会造成目标不明确，先仅连接目标画布。

连接时网页要保持打开，因为画布工具由网页执行；已经提交到服务器的图片任务在关闭网页后仍可继续。网页内的Codex聊天入口也可使用同一本地服务；外部Codex需要注册MCP，仅启动服务不会自动安装工具。

`Connect token`不是New API Key，不要发到聊天、文档或公开链接。服务仅监听本机回环，无须开放服务器或电脑公网端口。停止本地服务可在对应终端按Ctrl+C；移除MCP可执行 `codex mcp remove infinite-canvas`。

## 插件自动连接

与直接注册MCP二选一。当前Codex CLI支持插件命令时执行：

```powershell
codex plugin marketplace add https://github.com/tigerowo/infinite-canvas.git
codex plugin add canvas-agent@infinite-canvas
```

新建对话，明确告诉Codex本站地址或当前 `/canvas/<id>` 完整地址，例如“帮我打开并连接 https://canvas.happykai.cn 的画布”。插件使用上游发布的本机工具，未修改其协议；上游插件后续版本需要另行组合验收。插件自动打开方式的浏览器权限、站点登录仍需正常可用。

参考：[上游Canvas Agent说明](../../canvas-agent/README.md)、[OpenAI官方Codex MCP文档](https://developers.openai.com/codex/mcp/)、[官方CLI MCP注册实现](https://github.com/openai/codex/blob/main/codex-rs/cli/src/mcp_cmd.rs)。本站“Codex CLI接口兼容”仅调整某些生成接口的参数，不等于上述MCP连接。
