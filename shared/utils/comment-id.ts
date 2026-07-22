/**
 * Extract the stable WeChat comment identifier from supported article templates.
 * This helper is intentionally storage-free so protocol validation can run in
 * Node, the browser, and future Go parity tooling without importing Dexie.
 */
export function extractCommentId(html: string): string | null {
  const patterns = [
    /var comment_id = '(?<comment_id>\d+)' \|\| '0';/,
    /comment_id:\s*JsDecode\('(?<comment_id>\d+)'\)/,
    /d\.comment_id\s*=\s*xml \? getXmlValue\('comment_id\.DATA'\) : '(?<comment_id>\d+)';/,
    /window\.comment_id\s*=\s*'(?<comment_id>\d+)'/,
  ];

  for (const pattern of patterns) {
    const match = html.match(pattern);
    if (match?.groups?.comment_id) return match.groups.comment_id;
    if (match?.[1]) return match[1];
  }
  return null;
}
