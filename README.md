# TarsOpenWebUI

MCP integration for OpenWebUI (self-hosted LLM chat).

## Tools

- `openwebui_list_models` — List available local models
- `openwebui_chat` — Send a chat completion request

## Quick Start

```bash
go build -o tarsopenwebui
TARS_MCP_ENDPOINT=https://api.example.com TARS_MCP_API_KEY=xxx ./tarsopenwebui
```

## Environment Variables

- `TARS_MCP_ENDPOINT` — upstream API endpoint
- `TARS_MCP_API_KEY` — upstream API key
- `TARS_MCP_MODE` — `stdio` (default) or `http`

## License

MIT
