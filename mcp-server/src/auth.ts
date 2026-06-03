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
  ISSUER_BASE_URL: string;
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

    if (url.pathname === '/authorize') {
      if (request.method === 'GET') {
        const oauthReq = await env.OAUTH_PROVIDER.parseAuthRequest(request);
        const state = btoa(unescape(encodeURIComponent(JSON.stringify(oauthReq))));
        return html(renderConsent(state, env.EXPORTER_BASE_URL));
      }
      if (request.method === 'POST') {
        const form = await request.formData();
        const oauthReq = JSON.parse(decodeURIComponent(escape(atob(String(form.get('state')))))) as AuthRequest;
        const apiToken = String(form.get('auth_key') || '').trim();
        if (!apiToken) return html(renderConsent('', env.EXPORTER_BASE_URL, '请填写 auth_key'), 400);

        // 校验 token 有效：调一个需鉴权的轻量接口（401 即无效）
        const ok = await verifyToken(apiToken, env.EXPORTER_BASE_URL);
        if (!ok) {
          const state = btoa(unescape(encodeURIComponent(JSON.stringify(oauthReq))));
          return html(renderConsent(state, env.EXPORTER_BASE_URL, 'auth_key 无效或已过期，请重新登录复制'), 401);
        }

        const { redirectTo } = await env.OAUTH_PROVIDER.completeAuthorization({
          request: oauthReq,
          userId: 'wx_' + apiToken.slice(0, 8),
          scope: oauthReq.scope?.length ? oauthReq.scope : ['wechat.read'],
          props: { apiToken },
        });
        return Response.redirect(redirectTo, 302);
      }
    }

    // 健康检查 / 根路径
    if (url.pathname === '/' || url.pathname === '/health') {
      return Response.json({ service: 'wechat-article-mcp', status: 'ok' });
    }
    return new Response('Not found', { status: 404 });
  },
} satisfies ExportedHandler<Env>;

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
  return new Response(body, { status, headers: { 'content-type': 'text/html; charset=utf-8' } });
}

function renderConsent(state: string, exporterUrl: string, error = ''): string {
  return `<!doctype html><html lang=zh><head><meta charset=utf-8>
<meta name=viewport content="width=device-width,initial-scale=1">
<title>授权 · 微信文章导出 MCP</title>
<style>
 body{font:15px/1.6 system-ui,sans-serif;max-width:520px;margin:8vh auto;padding:0 20px;color:#1f2937}
 h1{font-size:20px} ol{padding-left:20px} a{color:#2563eb}
 input[name=auth_key]{width:100%;box-sizing:border-box;padding:10px;font:13px monospace;border:1px solid #d1d5db;border-radius:8px;margin:8px 0}
 button{background:#2563eb;color:#fff;border:0;border-radius:8px;padding:10px 18px;font-size:15px;cursor:pointer}
 .err{color:#dc2626;margin:8px 0} .muted{color:#6b7280;font-size:13px}
</style></head><body>
<h1>授权 AI 访问你的「微信文章导出」</h1>
${error ? `<p class=err>⚠ ${error}</p>` : ''}
<ol>
 <li>打开 <a href="${exporterUrl}" target=_blank rel=noopener>${exporterUrl}</a> 扫码登录公众号</li>
 <li>在「设置」页复制你的 <code>auth_key</code></li>
 <li>粘贴到下方并授权（仅本次会话使用，服务端不长期保存）：</li>
</ol>
<form method=post action=/authorize>
 <input type=hidden name=state value="${state}">
 <input name=auth_key placeholder="粘贴 auth_key" autocomplete=off required>
 <button type=submit>授权</button>
</form>
<p class=muted>授权后 AI 客户端将获得一个受限访问令牌（含本次微信会话），过期需重新授权。</p>
</body></html>`;
}
