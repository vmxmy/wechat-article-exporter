/**
 * 授权处理器（OAuthProvider 的 defaultHandler）。
 * /authorize：渲染同意页 → 用户提供微信凭证 → completeAuthorization(props) → 重定向回客户端。
 *
 * Phase 1（本文件）：用户先到 exporter 扫码登录、复制 auth_key 粘贴授权（最小改动、可跑通 OAuth 全链路）。
 * Phase 2（TODO）：把 exporter 的扫码二维码直接内嵌到本同意页（getqrcode→轮询→bizlogin），
 *                  并把 props 换成自包含的 {token, cookies}，彻底去掉 exporter 的会话 KV，实现全无状态。
 */
export interface Env {
  EXPORTER_BASE_URL: string;
	// Optional compatibility-window controls. Operators set the deadline when
	// the retirement release is announced; until then behavior is unchanged.
	REMOTE_OAUTH_DISABLE_AFTER?: string;
	LOCAL_CLI_MIGRATION_URL?: string;
  OAUTH_PROVIDER: {
    parseAuthRequest(request: Request): Promise<AuthRequest>;
    completeAuthorization(opts: {
      request: AuthRequest;
      userId: string;
      scope: string[];
      props: Record<string, unknown>;
      metadata?: Record<string, unknown>;
    }): Promise<{ redirectTo: string }>;
  };
}

interface AuthRequest {
  scope?: string[];
  [k: string]: unknown;
}

export const authHandler = {
  async fetch(request, env): Promise<Response> {
    const url = new URL(request.url);
	const migration = remoteOAuthMigration(env);

    if (url.pathname === '/authorize') {
		if (migration.disabled) return migrationResponse(migration, 410);
      if (request.method === 'GET') {
        const oauthReq = await env.OAUTH_PROVIDER.parseAuthRequest(request);
        const state = encodeAuthorizationState(oauthReq);
        return html(renderConsent(state, env.EXPORTER_BASE_URL, '', url.searchParams.get('headless') === '1'));
      }
      if (request.method === 'POST') {
        const form = await request.formData();
        const state = String(form.get('state') || '');
        const headless = form.get('headless') === '1';
        let oauthReq: AuthRequest;
        try {
          oauthReq = decodeAuthorizationState(state);
        } catch {
          return html(
            renderConsent('', env.EXPORTER_BASE_URL, '授权请求已失效，请从 MCP 客户端重新发起登录', headless),
            400
          );
        }

        const apiToken = String(form.get('auth_key') || '').trim();
        if (!apiToken) return html(renderConsent(state, env.EXPORTER_BASE_URL, '请填写 auth_key', headless), 400);

        // 校验 token 有效：调一个需鉴权的轻量接口（401 即无效）
        const ok = await verifyToken(apiToken, env.EXPORTER_BASE_URL);
        if (!ok) {
          return html(
            renderConsent(state, env.EXPORTER_BASE_URL, 'auth_key 无效或已过期，请重新登录复制', headless),
            401
          );
        }

        const { redirectTo } = await env.OAUTH_PROVIDER.completeAuthorization({
          request: oauthReq,
          userId: 'wx_' + apiToken.slice(0, 8),
          scope: oauthReq.scope?.length ? oauthReq.scope : ['wechat.read'],
          props: { apiToken },
        });
        if (headless && isLoopbackRedirect(redirectTo)) return html(renderHeadlessCompletion(redirectTo));
        return redirect(redirectTo);
      }
    }

    // 健康检查 / 根路径
    if (url.pathname === '/' || url.pathname === '/health') {
		return Response.json({
			service: 'wechat-article-mcp',
			status: migration.disabled ? 'migration-only' : 'ok',
			remoteOAuth: migration.disabled ? 'disabled' : 'compatibility-window',
			migration: migration.url,
		});
    }
    return new Response('Not found', { status: 404 });
  },
} satisfies ExportedHandler<Env>;

interface RemoteOAuthMigration {
	disabled: boolean;
	deadline?: string;
	url: string;
}

function remoteOAuthMigration(env: Env): RemoteOAuthMigration {
	const raw = env.REMOTE_OAUTH_DISABLE_AFTER?.trim();
	const deadline = raw ? new Date(raw) : undefined;
	return {
		disabled: Boolean(deadline && !Number.isNaN(deadline.getTime()) && Date.now() >= deadline.getTime()),
		deadline: deadline && !Number.isNaN(deadline.getTime()) ? deadline.toISOString() : undefined,
		url: env.LOCAL_CLI_MIGRATION_URL?.trim() || 'https://github.com/wechat-article/wechat-article-exporter#local-cli',
	};
}

function migrationResponse(migration: RemoteOAuthMigration, status: number): Response {
	return Response.json(
		{
			error: 'remote_oauth_retired',
			message: 'Remote OAuth no longer accepts new authorizations. Install the local wechat-article binary, create a profile, and log in with a local WeChat QR code.',
			deadline: migration.deadline,
			migration: migration.url,
			command: 'wechat-article login',
		},
		{ status, headers: { 'cache-control': 'no-store' } }
	);
}

async function verifyToken(apiToken: string, base: string): Promise<boolean> {
  try {
    // accountbyurl 不需鉴权，换一个需鉴权的：用 search 空跑探活更稳妥。
    // 这里用 /api/public/v1/account 关键字探活，401/鉴权失败即视为无效。
    const res = await fetch(`${base}/api/public/v1/account?keyword=test&begin=0&size=1`, {
      headers: { 'X-Auth-Key': apiToken },
    });
    if (!res.ok) return false;
    const body = (await res.json().catch(() => null)) as { base_resp?: { ret?: number } } | null;
    // ret !== 0 多为鉴权/会话失效
    return !(body?.base_resp && body.base_resp.ret !== 0);
  } catch {
    return false;
  }
}

function html(body: string, status = 200): Response {
  return new Response(body, {
    status,
    headers: {
      'cache-control': 'no-store',
      'content-security-policy':
        "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'",
      'content-type': 'text/html; charset=utf-8',
      'referrer-policy': 'no-referrer',
      'x-content-type-options': 'nosniff',
    },
  });
}

function redirect(location: string): Response {
  return new Response(null, {
    status: 302,
    headers: {
      'cache-control': 'no-store',
      location,
      'referrer-policy': 'no-referrer',
    },
  });
}

function renderConsent(state: string, exporterUrl: string, error = '', headless = false): string {
  const safeExporterUrl = escapeHtml(exporterUrl);
  return `<!doctype html><html lang=zh><head><meta charset=utf-8>
<meta name=viewport content="width=device-width,initial-scale=1">
<title>授权 · 微信文章导出 MCP</title>
<style>
 body{font:15px/1.6 system-ui,sans-serif;max-width:520px;margin:8vh auto;padding:0 20px;color:#1f2937}
 h1{font-size:20px} ol{padding-left:20px} a{color:#2563eb}
 input[name=auth_key]{width:100%;box-sizing:border-box;padding:10px;font:13px monospace;border:1px solid #d1d5db;border-radius:8px;margin:8px 0}
 button{background:#2563eb;color:#fff;border:0;border-radius:8px;padding:10px 18px;font-size:15px;cursor:pointer}
 .headless{display:flex;gap:8px;align-items:flex-start;margin:8px 0 16px}.headless input{margin-top:5px}
 .err{color:#dc2626;margin:8px 0}.muted{color:#6b7280;font-size:13px}
</style></head><body>
<p class=muted>Remote OAuth 已进入兼容迁移期，计划最早于 2026-12-31 退役，且必须先满足稳定本地版和功能对等门禁。新工作流请安装本地 <code>wechat-article</code> 并扫码登录。</p>
<h1>授权 AI 访问你的「微信文章导出」</h1>
${error ? `<p class=err>⚠ ${escapeHtml(error)}</p>` : ''}
<ol>
 <li>打开 <a href="${safeExporterUrl}" target=_blank rel="noopener noreferrer">${safeExporterUrl}</a> 扫码登录公众号</li>
 <li>在「设置」页复制你的 <code>auth_key</code></li>
 <li>粘贴到下方并授权（凭证会加密绑定到本次 OAuth 授权，KV 不保存可直接读取的明文）：</li>
</ol>
<form method=post action=/authorize>
 <input type=hidden name=state value="${escapeHtml(state)}">
 <input name=auth_key placeholder="粘贴 auth_key" autocomplete=off spellcheck=false required>
 <label class=headless>
  <input type=checkbox name=headless value=1${headless ? ' checked' : ''}>
  <span>我的 MCP 客户端运行在无浏览器服务器上；授权后显示一条可复制回服务器执行的回调命令。</span>
 </label>
 <button type=submit>授权</button>
</form>
<p class=muted>访问令牌有效期 7 天，客户端可在 180 天内自动刷新；通常无需重新打开浏览器。</p>
</body></html>`;
}

function renderHeadlessCompletion(redirectTo: string): string {
  const callbackUrl = escapeHtml(redirectTo);
  const command = escapeHtml(`curl -fsS --max-time 30 ${shellQuote(redirectTo)}`);
  return `<!doctype html><html lang=zh><head><meta charset=utf-8>
<meta name=viewport content="width=device-width,initial-scale=1">
<title>在服务器上完成授权 · 微信文章导出 MCP</title>
<style>
 body{font:15px/1.6 system-ui,sans-serif;max-width:720px;margin:8vh auto;padding:0 20px;color:#1f2937}
 h1{font-size:20px}pre{padding:14px;overflow:auto;background:#111827;color:#f9fafb;border-radius:8px;white-space:pre-wrap;word-break:break-all}
 a{color:#2563eb}.warn{color:#92400e;background:#fffbeb;padding:10px 12px;border-radius:8px}
</style></head><body>
<h1>在服务器上完成最后一步</h1>
<p>复制下面的命令，回到正在等待 OAuth 登录的服务器终端执行：</p>
<pre>${command}</pre>
<p class=warn>该命令包含短期有效、仅可使用一次的授权码。请勿分享到聊天或日志中。</p>
<p>如果浏览器与 MCP 客户端在同一台机器，也可以<a href="${callbackUrl}" rel="noreferrer">直接完成授权</a>。</p>
</body></html>`;
}

export function encodeAuthorizationState(value: AuthRequest): string {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

export function decodeAuthorizationState(value: string): AuthRequest {
  const binary = atob(value);
  const bytes = Uint8Array.from(binary, char => char.charCodeAt(0));
  const parsed = JSON.parse(new TextDecoder().decode(bytes)) as unknown;
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('Invalid authorization state');
  return parsed as AuthRequest;
}

export function isLoopbackRedirect(value: string): boolean {
  try {
    const hostname = new URL(value).hostname.toLowerCase();
    return hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '[::1]' || hostname === '::1';
  } catch {
    return false;
  }
}

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, char => {
    const replacements: Record<string, string> = {
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;',
    };
    return replacements[char];
  });
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}
