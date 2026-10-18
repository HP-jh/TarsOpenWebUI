# TarsOpenWebUI

MCP integration for OpenWebUI (self-hosted LLM chat).

**当前版本：v1.1.0**

## 关于作者

> 你好！我是这个项目的作者，一名正在读小学的学生。
> 这个项目从规划到代码实现，**完全由我自己主导设计，并借助 AI 工具完成**。每一行代码、每一个功能模块，都凝聚了我对技术的热爱与坚持。
> 如果你认可这个项目，或者觉得它对你有帮助，希望能给我一个 **Star** 支持，这对我意义重大！

## 欢迎支持

- 如果你在使用过程中遇到问题，欢迎提交 [Issue](https://github.com/HP-jh) 或参与讨论
- 如果你觉得项目不错，请点一个 **Star**，这是对我最大的鼓励
- 也欢迎分享给身边有需要的朋友，让更多人看到这个项目
- 后续功能迭代会继续推进，你的支持是我持续更新的动力

## ⚠️ 维护说明

> **本项目依赖 AI 服务套餐，若因套餐额度耗尽或服务商调整，可能导致更新暂时停滞。**
> 如遇较长时间未更新，请理解这是 AI 服务限制所致，并非项目放弃维护。
> 恢复服务后会第一时间继续推进，感谢你的耐心与包容。

---

## Tools

- `openwebui_list_models` — 列出 OpenWebUI 中可用的本地模型
- `openwebui_chat` — 向指定模型发送对话请求并获取回复

## Quick Start

```bash
go build -o tarsopenwebui
TARS_MCP_ENDPOINT=https://api.example.com TARS_MCP_API_KEY=xxx ./tarsopenwebui
```

## Environment Variables

- `TARS_MCP_ENDPOINT` — 上游 API 地址（**必填**）
- `TARS_MCP_API_KEY` — 上游 API 密钥
- `TARS_MCP_MODE` — `stdio`（默认）或 `http`

## License

MIT
