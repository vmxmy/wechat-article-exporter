import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, '../..');
const targets = [
  path.join(scriptDirectory, 'article-processing.v1.json'),
  path.join(repositoryRoot, 'test/fixtures/protocol'),
];

const secretPatterns = [
  /"(?:access[_-]?token|refresh[_-]?token|appmsg_token|pass_ticket|client_secret|authorization|cookie|key)"\s*:\s*"(?!<redacted>|example|fixture)[^"\n]{4,}"/gi,
  /(?:access_token|refresh_token|appmsg_token|pass_ticket|client_secret|authorization|cookie)=((?!<redacted>|example|fixture)[^&\s"']{4,})/gi,
];

function filesUnder(target: string): string[] {
  if (!fs.existsSync(target)) return [];
  const stat = fs.statSync(target);
  if (stat.isFile()) return [target];
  return fs.readdirSync(target, { withFileTypes: true }).flatMap(entry => filesUnder(path.join(target, entry.name)));
}

const findings: string[] = [];
for (const target of targets.flatMap(filesUnder)) {
  if (!/\.(?:json|txt|md)$/i.test(target)) continue;
  const content = fs.readFileSync(target, 'utf8');
  for (const pattern of secretPatterns) {
    pattern.lastIndex = 0;
    if (pattern.test(content)) findings.push(path.relative(repositoryRoot, target));
  }
}

if (findings.length > 0) {
  throw new Error(`baseline redaction check failed: ${[...new Set(findings)].join(', ')}`);
}
console.log('baseline redaction check passed');
