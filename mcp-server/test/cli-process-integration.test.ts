import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, stat } from 'node:fs/promises';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js';
import { z } from 'zod';
import { writeCliConfig } from '../src/cli/config.ts';

const execFileAsync = promisify(execFile);
const cwd = path.resolve(import.meta.dirname, '..');
const cli = path.join(cwd, 'src/cli/wechat-article.ts');

test('CLI process reuses saved OAuth, discovers tools, calls aliases, and never prints tokens', async t => {
  const httpServer = createServer(async (request, response) => {
    if (request.url !== '/mcp' || request.headers.authorization !== 'Bearer process-test-token') {
      response.writeHead(request.url === '/mcp' ? 401 : 404).end();
      return;
    }
    const server = new McpServer({ name: 'process-test', version: '1.0.0' });
    server.tool(
      'download_article',
      'download article',
      { url: z.string(), format: z.enum(['markdown', 'text', 'html', 'json']).default('markdown') },
      async ({ url, format }) => ({ content: [{ type: 'text', text: `${format}:${url}` }] })
    );
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
  const serverUrl = `http://127.0.0.1:${address.port}`;

  const directory = await mkdtemp(path.join(tmpdir(), 'wechat-article-cli-'));
  const configPath = path.join(directory, 'cli.json');
  await writeCliConfig(
    {
      server: serverUrl,
      tokens: { access_token: 'process-test-token', token_type: 'bearer', refresh_token: 'refresh-secret' },
    },
    configPath
  );
  assert.equal((await stat(configPath)).mode & 0o777, 0o600);
  const env = { ...process.env, WECHAT_ARTICLE_CLI_CONFIG: configPath };

  const status = await runCli(['status'], env);
  assert.equal(JSON.parse(status.stdout).data.authenticated, true);
  assert.equal(status.stdout.includes('process-test-token'), false);
  assert.equal(status.stdout.includes('refresh-secret'), false);

  const list = await runCli(['api', 'list'], env);
  assert.deepEqual(
    JSON.parse(list.stdout).data.tools.map((tool: { name: string }) => tool.name),
    ['download_article']
  );

  const call = await runCli(['article', 'download', 'https://mp.weixin.qq.com/s/example', '--format', 'text'], env);
  assert.equal(JSON.parse(call.stdout).content[0].text, 'text:https://mp.weixin.qq.com/s/example');
});

async function runCli(args: string[], env: NodeJS.ProcessEnv) {
  return execFileAsync(process.execPath, ['--experimental-strip-types', cli, ...args], { cwd, env });
}
