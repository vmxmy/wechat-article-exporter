import type { OAuthClientProvider } from '@modelcontextprotocol/sdk/client/auth.js';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import type { CliToolDescriptor } from './safety.ts';

export interface CliMcpTool extends CliToolDescriptor {
  description?: string;
  [key: string]: unknown;
}

export interface CliMcpCallResult {
  content: unknown[];
  isError?: boolean;
  structuredContent?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface CliMcpClientOptions {
  server: string;
  version: string;
  authProvider?: OAuthClientProvider;
  fetch?: typeof fetch;
}

export class CliMcpClient {
  private readonly client: Client;
  private readonly transport: StreamableHTTPClientTransport;
  private connected = false;

  constructor(options: CliMcpClientOptions) {
    this.client = new Client({ name: 'wechat-article-cli', version: options.version }, { capabilities: {} });
    this.transport = new StreamableHTTPClientTransport(mcpUrl(options.server), {
      authProvider: options.authProvider,
      fetch: options.fetch,
    });
  }

  async listTools(): Promise<CliMcpTool[]> {
    await this.connect();
    const tools: CliMcpTool[] = [];
    let cursor: string | undefined;
    do {
      const page = await this.client.listTools(cursor ? { cursor } : undefined);
      tools.push(...(page.tools as CliMcpTool[]));
      cursor = page.nextCursor;
    } while (cursor);
    return tools;
  }

  async callTool(name: string, args: Record<string, unknown>): Promise<CliMcpCallResult> {
    await this.connect();
    return (await this.client.callTool({ name, arguments: args })) as CliMcpCallResult;
  }

  async finishAuth(code: string): Promise<void> {
    await this.transport.finishAuth(code);
  }

  async close(): Promise<void> {
    if (!this.connected) {
      await this.transport.close();
      return;
    }
    this.connected = false;
    await this.client.close();
  }

  private async connect(): Promise<void> {
    if (this.connected) return;
    await this.client.connect(this.transport);
    this.connected = true;
  }
}

function mcpUrl(server: string): URL {
  const url = new URL(server);
  if (url.pathname === '/mcp') return url;
  return new URL('/mcp', url);
}
