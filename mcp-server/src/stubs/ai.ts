/**
 * Stub for the optional `ai` (Vercel AI SDK) dynamic import inside `agents`.
 * 仅在 agents 的客户端 jsonSchema 转换路径用到；本服务端 MCP（zod 工具）不触发。
 * 用 wrangler [alias] 把 "ai" 指到这里，避免把整个 AI SDK 打进 worker。
 */
export const jsonSchema = (schema: unknown): unknown => schema;
export default {} as Record<string, unknown>;
