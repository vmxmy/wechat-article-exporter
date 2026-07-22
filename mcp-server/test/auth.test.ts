import assert from 'node:assert/strict';
import { afterEach, test } from 'node:test';
import { authHandler, decodeAuthorizationState, encodeAuthorizationState, isLoopbackRedirect } from '../src/auth.ts';
import {
  OAUTH_ACCESS_TOKEN_TTL_SECONDS,
  OAUTH_CLIENT_REGISTRATION_TTL_SECONDS,
  OAUTH_REFRESH_TOKEN_TTL_SECONDS,
} from '../src/constants.ts';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

test('OAuth 生命周期减少频繁授权且客户端注册不会先过期', () => {
  assert.equal(OAUTH_ACCESS_TOKEN_TTL_SECONDS, 7 * 24 * 60 * 60);
  assert.equal(OAUTH_REFRESH_TOKEN_TTL_SECONDS, 180 * 24 * 60 * 60);
  assert.ok(OAUTH_CLIENT_REGISTRATION_TTL_SECONDS > OAUTH_REFRESH_TOKEN_TTL_SECONDS);
});

test('授权请求状态支持 Unicode 往返', () => {
  const request = { clientId: '客户端', scope: ['wechat.read'], redirectUri: 'http://127.0.0.1:4321/callback' };
  assert.deepEqual(decodeAuthorizationState(encodeAuthorizationState(request)), request);
  assert.throws(() => decodeAuthorizationState(btoa('[]')));
});

test('仅 loopback 回调进入 headless 手动完成模式', () => {
  assert.equal(isLoopbackRedirect('http://127.0.0.1:4321/callback'), true);
  assert.equal(isLoopbackRedirect('http://[::1]:4321/callback'), true);
  assert.equal(isLoopbackRedirect('https://client.example.com/callback'), false);
  assert.equal(isLoopbackRedirect('not a url'), false);
});

test('GET /authorize 可预选无浏览器服务器模式', async () => {
  const response = await authHandler.fetch(
    new Request('https://mptext.ziikoo.app/authorize?headless=1'),
    createEnv('http://127.0.0.1:4321/callback')
  );
  const body = await response.text();

  assert.equal(response.status, 200);
  assert.match(response.headers.get('cache-control') || '', /no-store/);
  assert.match(body, /name=headless value=1 checked/);
});

test('公告截止时间后拒绝新授权并返回本地迁移指引', async () => {
	const env = createEnv('http://127.0.0.1:4321/callback');
	const response = await authHandler.fetch(new Request('https://mptext.ziikoo.app/authorize'), {
		...env,
		REMOTE_OAUTH_DISABLE_AFTER: '2000-01-01T00:00:00Z',
		LOCAL_CLI_MIGRATION_URL: 'https://example.test/local-cli',
	});
	const body = await response.json() as Record<string, unknown>;
	assert.equal(response.status, 410);
	assert.equal(body.error, 'remote_oauth_retired');
	assert.equal(body.migration, 'https://example.test/local-cli');
	assert.equal(body.command, 'wechat-article login');
});

test('普通授权继续直接跳转到客户端回调', async () => {
  globalThis.fetch = async () => Response.json({ base_resp: { ret: 0 } });
  const redirectTo = 'http://127.0.0.1:4321/callback?code=one-time-code&state=client-state';
  const response = await authHandler.fetch(createAuthorizationPost(redirectTo, false), createEnv(redirectTo));

  assert.equal(response.status, 302);
  assert.equal(response.headers.get('location'), redirectTo);
  assert.match(response.headers.get('cache-control') || '', /no-store/);
});

test('损坏的授权状态返回 400 而不是触发服务端异常', async () => {
  const body = new URLSearchParams({ state: 'not-valid-base64', auth_key: 'valid-auth-key', headless: '1' });
  const response = await authHandler.fetch(
    new Request('https://mptext.ziikoo.app/authorize', {
      method: 'POST',
      headers: { 'content-type': 'application/x-www-form-urlencoded' },
      body,
    }),
    createEnv('http://127.0.0.1:4321/callback')
  );

  assert.equal(response.status, 400);
  assert.match(await response.text(), /授权请求已失效/);
});

test('headless 授权显示可复制到服务器执行的一次性回调命令', async () => {
  globalThis.fetch = async () => Response.json({ base_resp: { ret: 0 } });
  const redirectTo = 'http://127.0.0.1:4321/callback?code=one-time-code&state=client-state';
  const response = await authHandler.fetch(createAuthorizationPost(redirectTo, true), createEnv(redirectTo));
  const body = await response.text();

  assert.equal(response.status, 200);
  assert.match(response.headers.get('content-security-policy') || '', /frame-ancestors 'none'/);
  assert.match(body, /curl -fsS --max-time 30/);
  assert.match(body, /127\.0\.0\.1:4321/);
  assert.match(body, /仅可使用一次的授权码/);
});

function createAuthorizationPost(redirectTo: string, headless: boolean): Request {
  const state = encodeAuthorizationState({
    clientId: 'test-client',
    redirectUri: redirectTo,
    scope: ['wechat.read'],
  });
  const body = new URLSearchParams({ state, auth_key: 'valid-auth-key' });
  if (headless) body.set('headless', '1');
  return new Request('https://mptext.ziikoo.app/authorize', {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body,
  });
}

function createEnv(redirectTo: string) {
  return {
    EXPORTER_BASE_URL: 'https://mp.ziikoo.app',
    OAUTH_PROVIDER: {
      parseAuthRequest: async () => ({
        clientId: 'test-client',
        redirectUri: redirectTo,
        scope: ['wechat.read'],
      }),
      completeAuthorization: async () => ({ redirectTo }),
    },
  };
}
