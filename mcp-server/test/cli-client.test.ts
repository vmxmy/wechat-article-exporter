import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import test from 'node:test';
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js';
import { z } from 'zod';
import { CliMcpClient } from '../src/cli/client.ts';

test('authenticated CLI client lists and calls the server-advertised tool surface', async t => {
  let authenticatedRequests = 0;
  const httpServer = createServer(async (request, response) => {
    if (request.url !== '/mcp') {
      response.writeHead(404).end();
      return;
    }
    if (request.headers.authorization !== 'Bearer test-token') {
      response.writeHead(401).end();
      return;
    }
    authenticatedRequests += 1;
    const server = new McpServer({ name: 'cli-test', version: '1.0.0' });
    server.tool('download_article', 'download test tool', { url: z.string() }, async ({ url }) => ({
      content: [{ type: 'text', text: `downloaded=${url}` }],
    }));
    const transport = new StreamableHTTPServerTransport({
      sessionIdGenerator: undefined,
      enableJsonResponse: true,
    });
    await server.connect(transport);
    await transport.handleRequest(request, response);
    response.on('close', () => {
      void transport.close();
      void server.close();
    });
  });
  await new Promise<void>(resolve => httpServer.listen(0, '127.0.0.1', resolve));
  t.after(() => httpServer.close());
  const address = httpServer.address() as AddressInfo;

  const client = new CliMcpClient({
    server: `http://127.0.0.1:${address.port}`,
    version: 'test',
    fetch: async (input, init = {}) => {
      const headers = new Headers(init.headers);
      headers.set('authorization', 'Bearer test-token');
      return fetch(input, { ...init, headers });
    },
  });
  t.after(() => client.close());

  const tools = await client.listTools();
  assert.deepEqual(
    tools.map(tool => tool.name),
    ['download_article']
  );
  const result = await client.callTool('download_article', { url: 'https://mp.weixin.qq.com/s/example' });
  assert.equal((result.content[0] as { text?: string }).text, 'downloaded=https://mp.weixin.qq.com/s/example');
  assert.ok(authenticatedRequests >= 3);
});
