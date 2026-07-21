import { randomBytes } from 'node:crypto';
import type { OAuthClientProvider } from '@modelcontextprotocol/sdk/client/auth.js';
import type {
  OAuthClientInformationMixed,
  OAuthClientMetadata,
  OAuthTokens,
} from '@modelcontextprotocol/sdk/shared/auth.js';
import { readCliConfig, writeCliConfig } from './config.ts';

interface FileOAuthProviderOptions {
  server: string;
  redirectUrl: string;
  configPath?: string;
  onRedirect?: (authorizationUrl: URL) => void | Promise<void>;
}

export class FileOAuthProvider implements OAuthClientProvider {
  readonly clientMetadataUrl = undefined;
  private readonly server: string;
  private readonly configPath?: string;
  private readonly onRedirect?: (authorizationUrl: URL) => void | Promise<void>;
  private readonly callbackUrl: string;

  constructor(options: FileOAuthProviderOptions) {
    this.server = normalizeServer(options.server);
    this.callbackUrl = options.redirectUrl;
    this.configPath = options.configPath;
    this.onRedirect = options.onRedirect;
  }

  get redirectUrl(): string {
    return this.callbackUrl;
  }

  get clientMetadata(): OAuthClientMetadata {
    return {
      client_name: 'WeChat Article Exporter CLI',
      redirect_uris: [this.callbackUrl],
      grant_types: ['authorization_code', 'refresh_token'],
      response_types: ['code'],
      token_endpoint_auth_method: 'none',
      scope: 'wechat.read',
    };
  }

  async state(): Promise<string> {
    const state = randomBytes(24).toString('base64url');
    await this.update({ oauthState: state });
    return state;
  }

  async clientInformation(): Promise<OAuthClientInformationMixed | undefined> {
    const config = await this.currentConfig();
    return config.clientInformation;
  }

  async saveClientInformation(clientInformation: OAuthClientInformationMixed): Promise<void> {
    await this.update({ clientInformation });
  }

  async tokens(): Promise<OAuthTokens | undefined> {
    const config = await this.currentConfig();
    return config.tokens;
  }

  async saveTokens(tokens: OAuthTokens): Promise<void> {
    await this.update({ tokens, tokenSavedAt: Date.now() });
  }

  async redirectToAuthorization(authorizationUrl: URL): Promise<void> {
    await this.onRedirect?.(authorizationUrl);
  }

  async saveCodeVerifier(codeVerifier: string): Promise<void> {
    await this.update({ codeVerifier });
  }

  async codeVerifier(): Promise<string> {
    const config = await this.currentConfig();
    if (!config.codeVerifier) throw new Error('OAuth code verifier is missing. Restart `wechat-article login`.');
    return config.codeVerifier;
  }

  async invalidateCredentials(scope: 'all' | 'client' | 'tokens' | 'verifier'): Promise<void> {
    const config = await this.currentConfig();
    if (scope === 'all') {
      await writeCliConfig({ server: this.server }, this.configPath);
      return;
    }
    if (scope === 'client') delete config.clientInformation;
    if (scope === 'tokens') {
      delete config.tokens;
      delete config.tokenSavedAt;
    }
    if (scope === 'verifier') {
      delete config.codeVerifier;
      delete config.oauthState;
    }
    await writeCliConfig(config, this.configPath);
  }

  private async currentConfig() {
    const config = await readCliConfig(this.configPath);
    return normalizeServer(config.server || '') === this.server ? config : { server: this.server };
  }

  private async update(fields: Record<string, unknown>): Promise<void> {
    const config = await this.currentConfig();
    await writeCliConfig({ ...config, ...fields, server: this.server }, this.configPath);
  }
}

function normalizeServer(value: string): string {
  if (!value) return '';
  const url = new URL(value);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
    throw new Error('Server URL must use HTTP(S) and must not contain credentials.');
  }
  url.pathname = url.pathname === '/mcp' ? '' : url.pathname.replace(/\/$/, '');
  url.search = '';
  url.hash = '';
  return url.toString().replace(/\/$/, '');
}
