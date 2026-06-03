import TurndownService from 'turndown';
import { urlIsValidMpArticle } from '#shared/utils';
import { normalizeHtml, parseCgiDataNew } from '#shared/utils/html';
import { USER_AGENT } from '~/config';
import { getTokenFromEvent } from '~/server/services/api/auth-session';

interface SearchBizQuery {
  url: string;
  format: string;
}

export default defineEventHandler(async event => {
  const token = await getTokenFromEvent(event);
  if (!token) {
    return { base_resp: { ret: -1, err_msg: '认证信息无效' } };
  }

  const query = getQuery<SearchBizQuery>(event);
  if (!query.url) {
    return {
      base_resp: {
        ret: -1,
        err_msg: 'url不能为空',
      },
    };
  }

  let url: string;
  try {
    url = decodeURIComponent(query.url.trim());
  } catch {
    return { base_resp: { ret: -1, err_msg: 'url编码格式无效' } };
  }
  if (!urlIsValidMpArticle(url)) {
    return {
      base_resp: {
        ret: -1,
        err_msg: 'url不合法',
      },
    };
  }

  const format: string = (query.format || 'html').toLowerCase();
  if (!['html', 'markdown', 'text', 'json'].includes(format)) {
    return {
      base_resp: {
        ret: -1,
        err_msg: '不支持的format',
      },
    };
  }

  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), 30_000);
  let rawHtml: string;
  try {
    const res = await fetch(url, {
      headers: {
        Referer: 'https://mp.weixin.qq.com/',
        Origin: 'https://mp.weixin.qq.com',
        'User-Agent': USER_AGENT,
      },
      signal: ac.signal,
    });
    rawHtml = await res.text();
  } catch (err: any) {
    return { base_resp: { ret: -1, err_msg: `抓取文章失败: ${err?.message ?? String(err)}` } };
  } finally {
    clearTimeout(timer);
  }

  switch (format) {
    case 'html':
      return new Response(normalizeHtml(rawHtml, 'html'), {
        status: 200,
        headers: {
          'Content-Type': 'text/html; charset=UTF-8',
        },
      });
    case 'text':
      return new Response(normalizeHtml(rawHtml, 'text'), {
        status: 200,
        headers: {
          'Content-Type': 'text/plain; charset=UTF-8',
        },
      });
    case 'markdown':
      return new Response(new TurndownService().turndown(normalizeHtml(rawHtml, 'markdown')), {
        status: 200,
        headers: {
          'Content-Type': 'text/markdown; charset=UTF-8',
        },
      });
    case 'json':
      return await parseCgiDataNew(rawHtml);
    default:
      throw new Error(`Unknown format ${format}`);
  }
});
