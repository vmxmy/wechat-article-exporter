import { randomUUID } from 'node:crypto';
import { chmod, mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import path from 'node:path';
import type { OAuthClientInformationMixed, OAuthTokens } from '@modelcontextprotocol/sdk/shared/auth.js';

export interface CliConfig {
  server?: string;
  tokens?: OAuthTokens;
  clientInformation?: OAuthClientInformationMixed;
  codeVerifier?: string;
  oauthState?: string;
  tokenSavedAt?: number;
}

export function defaultCliConfigPath(): string {
  if (process.env.WECHAT_ARTICLE_CLI_CONFIG) return path.resolve(process.env.WECHAT_ARTICLE_CLI_CONFIG);
  const configRoot = process.env.XDG_CONFIG_HOME || path.join(homedir(), '.config');
  return path.join(configRoot, 'wechat-article-exporter', 'cli.json');
}

export async function readCliConfig(configPath = defaultCliConfigPath()): Promise<CliConfig> {
  try {
    const parsed = JSON.parse(await readFile(configPath, 'utf8')) as unknown;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
      throw new Error('CLI config must be an object.');
    return parsed as CliConfig;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return {};
    throw error;
  }
}

export async function writeCliConfig(config: CliConfig, configPath = defaultCliConfigPath()): Promise<void> {
  const directory = path.dirname(configPath);
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const temporaryPath = path.join(directory, `.${path.basename(configPath)}.${randomUUID()}.tmp`);
  await writeFile(temporaryPath, `${JSON.stringify(config, null, 2)}\n`, { mode: 0o600 });
  await rename(temporaryPath, configPath);
  await chmod(configPath, 0o600);
}

export async function clearCliSession(configPath = defaultCliConfigPath()): Promise<CliConfig> {
  const config = await readCliConfig(configPath);
  const cleared: CliConfig = { server: config.server };
  await writeCliConfig(cleared, configPath);
  return cleared;
}
