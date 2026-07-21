#!/usr/bin/env node
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { UnauthorizedError } from '@modelcontextprotocol/sdk/client/auth.js';
import { CliMcpClient, type CliMcpTool } from './client.ts';
import { clearCliSession, defaultCliConfigPath, readCliConfig } from './config.ts';
import { loadCliJsonObject } from './input.ts';
import { FileOAuthProvider } from './oauth-provider.ts';
import { assertToolConfirmation, createToolDryRun, requiredToolConfirmation } from './safety.ts';

interface ParsedArgs {
  command: string[];
  flags: Record<string, string | boolean>;
}

interface OAuthCallbackServer {
  redirectUrl: string;
  waitForCallback: Promise<{ code: string; state?: string }>;
  close: () => Promise<void>;
}

class CliUsageError extends Error {}

const CLI_VERSION = '2.0.0';
const DEFAULT_SERVER = 'https://mptext.ziikoo.app';

async function main(argv: string[]): Promise<void> {
  const parsed = parseArgs(argv);
  const [root, sub, leaf, fourth] = parsed.command;

  if (flagEnabled(parsed.flags, 'version') || flagEnabled(parsed.flags, 'v')) {
    console.log(CLI_VERSION);
    return;
  }
  if (!root || root === 'help' || flagEnabled(parsed.flags, 'help') || flagEnabled(parsed.flags, 'h')) {
    printHelp();
    return;
  }

  if (root === 'login') {
    await login(parsed.flags);
    return;
  }
  if (root === 'logout') {
    const config = await clearCliSession();
    printJson({ success: true, data: { server: config.server, authenticated: false } });
    return;
  }
  if (root === 'status' || root === 'whoami') {
    await printStatus(parsed.flags);
    return;
  }

  if (root === 'api') {
    await handleApiCommand(sub, leaf, parsed.flags);
    return;
  }
  if (root === 'mcp') {
    const apiCommand = sub === 'tools' ? 'list' : sub;
    await handleApiCommand(apiCommand, leaf, parsed.flags);
    return;
  }

  if (root === 'article' && sub === 'download') {
    const url = requiredValue(leaf, 'article download requires <url>.');
    await executeToolCall(
      'download_article',
      { url, format: stringFlag(parsed.flags, 'format') || 'markdown' },
      parsed.flags
    );
    return;
  }
  if (root === 'article' && sub === 'list') {
    const fakeid = requiredValue(leaf, 'article list requires <fakeid>.');
    await executeToolCall(
      'list_articles',
      compactObject({
        fakeid,
        keyword: stringFlag(parsed.flags, 'keyword'),
        begin: integerFlag(parsed.flags, 'begin', 0),
        size: integerFlag(parsed.flags, 'size', 5),
      }),
      parsed.flags
    );
    return;
  }
  if (root === 'account' && sub === 'search') {
    const keyword = requiredValue(leaf, 'account search requires <keyword>.');
    await executeToolCall(
      'search_accounts',
      { keyword, begin: integerFlag(parsed.flags, 'begin', 0), size: integerFlag(parsed.flags, 'size', 5) },
      parsed.flags
    );
    return;
  }
  if (root === 'account' && sub === 'from-url') {
    await executeToolCall(
      'get_account_by_url',
      { url: requiredValue(leaf, 'account from-url requires <url>.') },
      parsed.flags
    );
    return;
  }
  if (root === 'account' && sub === 'details') {
    await executeToolCall(
      'get_account_details',
      { fakeid: requiredValue(leaf, 'account details requires <fakeid>.') },
      parsed.flags
    );
    return;
  }
  if (root === 'account' && sub === 'author') {
    await executeToolCall(
      'get_author_info',
      { fakeid: requiredValue(leaf, 'account author requires <fakeid>.') },
      parsed.flags
    );
    return;
  }
  if (root === 'account' && sub === 'name') {
    await executeToolCall(
      'get_account_name',
      { url: requiredValue(leaf, 'account name requires <url>.') },
      parsed.flags
    );
    return;
  }
  if (root === 'album' && sub === 'list') {
    const fakeid = requiredValue(leaf, 'album list requires <fakeid> <albumId>.');
    const albumId = requiredValue(fourth, 'album list requires <fakeid> <albumId>.');
    await executeToolCall(
      'list_album',
      compactObject({
        fakeid,
        album_id: albumId,
        count: integerFlag(parsed.flags, 'count', 10),
        begin_msgid: stringFlag(parsed.flags, 'begin-msgid'),
        begin_itemidx: stringFlag(parsed.flags, 'begin-itemidx'),
      }),
      parsed.flags
    );
    return;
  }

  throw new CliUsageError(`Unknown command: ${parsed.command.join(' ')}`);
}

async function handleApiCommand(
  sub: string | undefined,
  toolName: string | undefined,
  flags: Record<string, string | boolean>
): Promise<void> {
  if (sub === 'list') {
    const client = await createAuthenticatedClient(flags);
    try {
      const tools = await client.listTools();
      printJson({
        success: true,
        data: {
          count: tools.length,
          tools: tools.map(tool => ({
            name: tool.name,
            description: tool.description,
            annotations: tool.annotations,
          })),
        },
      });
    } finally {
      await client.close();
    }
    return;
  }

  if (sub === 'describe') {
    if (!toolName) throw new CliUsageError('api describe requires a tool name.');
    const client = await createAuthenticatedClient(flags);
    try {
      const tool = (await client.listTools()).find(candidate => candidate.name === toolName);
      if (!tool) throw new Error(`MCP tool not found: ${toolName}`);
      printJson({ success: true, data: tool });
    } finally {
      await client.close();
    }
    return;
  }

  if (sub === 'call') {
    if (!toolName) throw new CliUsageError('api call requires a tool name.');
    const input = await structuredInput(flags);
    await executeToolCall(toolName, input, flags);
    return;
  }

  throw new CliUsageError(`Unknown API command: ${sub ?? ''}`.trim());
}

async function executeToolCall(
  toolName: string,
  args: Record<string, unknown>,
  flags: Record<string, string | boolean>
): Promise<void> {
  if (flagEnabled(flags, 'dry-run')) {
    const syntheticTool: CliMcpTool = { name: toolName, inputSchema: { type: 'object' } };
    printJson({
      ...createToolDryRun(toolName, args),
      requiredConfirmation: requiredToolConfirmation(syntheticTool),
    });
    return;
  }

  const client = await createAuthenticatedClient(flags);
  try {
    const tool = (await client.listTools()).find(candidate => candidate.name === toolName);
    if (!tool) throw new Error(`MCP tool not found: ${toolName}`);
    assertToolConfirmation(tool, stringFlag(flags, 'confirm'));
    const result = await client.callTool(tool.name, args);
    printJson(result);
    if (result.isError === true) process.exitCode = 1;
  } finally {
    await client.close();
  }
}

async function login(flags: Record<string, string | boolean>): Promise<void> {
  const existing = await readCliConfig();
  const server = normalizeServer(stringFlag(flags, 'server') || existing.server || DEFAULT_SERVER);
  const callback = await createOAuthCallbackServer();
  let authorizationStarted = false;
  const provider = new FileOAuthProvider({
    server,
    redirectUrl: callback.redirectUrl,
    onRedirect: authorizationUrl => {
      authorizationStarted = true;
      if (flagEnabled(flags, 'headless')) authorizationUrl.searchParams.set('headless', '1');
      console.error(`Open this authorization URL:\n${authorizationUrl.toString()}`);
      if (!flagEnabled(flags, 'no-open') && !flagEnabled(flags, 'headless')) openBrowser(authorizationUrl.toString());
    },
  });
  const client = new CliMcpClient({ server, version: CLI_VERSION, authProvider: provider });

  try {
    try {
      const tools = await client.listTools();
      printJson({ success: true, data: { server, authenticated: true, toolCount: tools.length } });
      return;
    } catch (error) {
      if (!(error instanceof UnauthorizedError) || !authorizationStarted) throw error;
    }

    const callbackResult = await callback.waitForCallback;
    const config = await readCliConfig();
    if (!config.oauthState || callbackResult.state !== config.oauthState) {
      throw new Error('OAuth callback state mismatch. Restart `wechat-article login`.');
    }
    await client.finishAuth(callbackResult.code);
    await client.close();

    const verifiedClient = new CliMcpClient({ server, version: CLI_VERSION, authProvider: provider });
    try {
      const tools = await verifiedClient.listTools();
      printJson({ success: true, data: { server, authenticated: true, toolCount: tools.length } });
    } finally {
      await verifiedClient.close();
    }
  } finally {
    await client.close();
    await callback.close();
  }
}

async function printStatus(flags: Record<string, string | boolean>): Promise<void> {
  const config = await readCliConfig();
  const server = normalizeServer(stringFlag(flags, 'server') || config.server || DEFAULT_SERVER);
  const sameServer = !config.server || normalizeServer(config.server) === server;
  printJson({
    success: true,
    data: {
      server,
      authenticated: sameServer && Boolean(config.tokens?.access_token),
      refreshable: sameServer && Boolean(config.tokens?.refresh_token),
      configPath: defaultCliConfigPath(),
    },
  });
}

async function createAuthenticatedClient(flags: Record<string, string | boolean>): Promise<CliMcpClient> {
  const config = await readCliConfig();
  const server = normalizeServer(stringFlag(flags, 'server') || config.server || DEFAULT_SERVER);
  if (!config.tokens?.access_token || (config.server && normalizeServer(config.server) !== server)) {
    throw new Error(`Not logged in to ${server}. Run: wechat-article login --server ${server}`);
  }
  const provider = new FileOAuthProvider({
    server,
    redirectUrl: 'http://127.0.0.1/callback',
  });
  return new CliMcpClient({ server, version: CLI_VERSION, authProvider: provider });
}

async function structuredInput(flags: Record<string, string | boolean>): Promise<Record<string, unknown>> {
  try {
    return await loadCliJsonObject({
      input: stringFlag(flags, 'input'),
      file: stringFlag(flags, 'file'),
      stdin: flagEnabled(flags, 'stdin'),
    });
  } catch (error) {
    throw new CliUsageError(error instanceof Error ? error.message : String(error));
  }
}

async function createOAuthCallbackServer(timeoutMs = 5 * 60 * 1000): Promise<OAuthCallbackServer> {
  let resolveCallback!: (value: { code: string; state?: string }) => void;
  let rejectCallback!: (reason: Error) => void;
  const waitForCallback = new Promise<{ code: string; state?: string }>((resolve, reject) => {
    resolveCallback = resolve;
    rejectCallback = reject;
  });
  const server = createServer((request, response) => {
    const url = new URL(request.url || '/', 'http://127.0.0.1');
    const code = url.searchParams.get('code');
    const error = url.searchParams.get('error');
    if (error) {
      response
        .writeHead(400, { 'content-type': 'text/plain; charset=utf-8' })
        .end('Authorization failed. Return to the CLI.');
      rejectCallback(new Error(`OAuth authorization failed: ${error}`));
      return;
    }
    if (!code) {
      response.writeHead(400, { 'content-type': 'text/plain; charset=utf-8' }).end('Missing authorization code.');
      return;
    }
    response
      .writeHead(200, { 'content-type': 'text/plain; charset=utf-8' })
      .end('Authorization complete. You can close this page.');
    resolveCallback({ code, state: url.searchParams.get('state') || undefined });
  });
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const timeout = setTimeout(() => rejectCallback(new Error('OAuth callback timed out.')), timeoutMs);
  const address = server.address() as AddressInfo;
  return {
    redirectUrl: `http://127.0.0.1:${address.port}/callback`,
    waitForCallback,
    close: async () => {
      clearTimeout(timeout);
      await new Promise<void>(resolve => server.close(() => resolve()));
    },
  };
}

function parseArgs(argv: string[]): ParsedArgs {
  const command: string[] = [];
  const flags: Record<string, string | boolean> = {};
  for (let index = 0; index < argv.length; index += 1) {
    const value = argv[index];
    if (!value.startsWith('-')) {
      command.push(value);
      continue;
    }
    const key = value.replace(/^-+/, '');
    const next = argv[index + 1];
    if (next !== undefined && !next.startsWith('-')) {
      flags[key] = next;
      index += 1;
    } else {
      flags[key] = true;
    }
  }
  return { command, flags };
}

function stringFlag(flags: Record<string, string | boolean>, name: string): string | undefined {
  const value = flags[name];
  return typeof value === 'string' ? value : undefined;
}

function flagEnabled(flags: Record<string, string | boolean>, name: string): boolean {
  const value = flags[name];
  return value === true || value === 'true' || value === 'yes' || value === '1';
}

function integerFlag(flags: Record<string, string | boolean>, name: string, fallback: number): number {
  const value = stringFlag(flags, name);
  if (value === undefined) return fallback;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 0) throw new CliUsageError(`--${name} must be a non-negative integer.`);
  return parsed;
}

function requiredValue(value: string | undefined, message: string): string {
  if (!value) throw new CliUsageError(message);
  return value;
}

function compactObject(value: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined));
}

function normalizeServer(value: string): string {
  const url = new URL(value);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
    throw new CliUsageError('Server URL must use HTTP(S) and must not contain credentials.');
  }
  if (url.pathname === '/mcp') url.pathname = '';
  url.search = '';
  url.hash = '';
  return url.toString().replace(/\/$/, '');
}

function openBrowser(url: string): void {
  const command = process.platform === 'darwin' ? 'open' : process.platform === 'win32' ? 'cmd' : 'xdg-open';
  const args = process.platform === 'win32' ? ['/c', 'start', '', url] : [url];
  try {
    const child = spawn(command, args, { detached: true, stdio: 'ignore' });
    child.unref();
  } catch {
    // The URL is always printed, which is the fallback for headless environments.
  }
}

function printJson(value: unknown): void {
  console.log(JSON.stringify(value, null, 2));
}

function printHelp(): void {
  console.log(`wechat-article — remote CLI for WeChat article export

Usage:
  wechat-article --version
  wechat-article login [--server <url>] [--headless | --no-open]
  wechat-article logout
  wechat-article status [--server <url>]
  wechat-article api list
  wechat-article api describe <tool>
  wechat-article api call <tool> [--input <json> | --file <path> | --stdin]
  wechat-article mcp tools
  wechat-article mcp describe <tool>
  wechat-article mcp call <tool> [--input <json> | --file <path> | --stdin]
  wechat-article article download <url> [--format markdown|text|html|json]
  wechat-article article list <fakeid> [--keyword <text>] [--begin 0] [--size 5]
  wechat-article account search <keyword> [--begin 0] [--size 5]
  wechat-article account from-url <url>
  wechat-article account details <fakeid>
  wechat-article account author <fakeid>
  wechat-article account name <url>
  wechat-article album list <fakeid> <albumId> [--count 10]

Runtime posture:
  - Remote-only: every operation uses the OAuth-protected Streamable HTTP /mcp endpoint.
  - api list/describe/call discovers and invokes the authoritative server tool surface.
  - Domain commands are thin aliases over the same MCP tools and schemas.
  - Prefer --file or --stdin for long or sensitive structured input.
  - Protected or newly discovered write-capable tools require exact --confirm <tool>.
  - --dry-run prints a redacted preview and never opens an MCP connection.
  - OAuth tokens are stored in a mode-0600 config file and are never printed.`);
}

main(process.argv.slice(2)).catch(error => {
  if ((error as NodeJS.ErrnoException)?.code === 'EPIPE') {
    process.exitCode = 0;
    return;
  }
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = error instanceof CliUsageError ? 2 : 1;
});
