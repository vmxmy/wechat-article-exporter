import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { normalizeHtml, validateHTMLContent } from '../../shared/utils/html';
import { read, samples } from '../common';

interface SampleBaseline {
  path: string;
  group: string;
  hasContent: boolean;
  sourceBytes: number;
  sourceSha256: string;
  validation: unknown;
  cgiScript: null | { bytes: number; sha256: string };
  normalized: null | { bytes: number; sha256: string };
  normalizedText: null | { bytes: number; sha256: string };
  normalizedMarkdownHtml: null | { bytes: number; sha256: string };
}

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, '../..');
const outputPath = path.join(scriptDirectory, 'article-processing.v1.json');
const verifyMode = process.argv.includes('--verify');

function digest(value: string): string {
  return crypto.createHash('sha256').update(value).digest('hex');
}

function describe(value: string) {
  return { bytes: Buffer.byteLength(value), sha256: digest(value) };
}

async function capture(): Promise<SampleBaseline[]> {
  const output: SampleBaseline[] = [];
  for (const group of samples) {
    for (const samplePath of group.samples) {
      const html = read(samplePath);
      const relativePath = path.relative(repositoryRoot, samplePath);
      const baseline: SampleBaseline = {
        path: relativePath,
        group: group.name,
        hasContent: group.hasContent,
        sourceBytes: Buffer.byteLength(html),
        sourceSha256: digest(html),
        validation: validateHTMLContent(html),
        cgiScript: null,
        normalized: null,
        normalizedText: null,
        normalizedMarkdownHtml: null,
      };

      if (group.hasContent) {
        const cgiScript = extractCgiScript(html);
        if (cgiScript) {
          baseline.cgiScript = describe(cgiScript);
        }
        baseline.normalized = describe(normalizeHtml(html));
        baseline.normalizedText = describe(normalizeHtml(html, 'text'));
        baseline.normalizedMarkdownHtml = describe(normalizeHtml(html, 'markdown'));
      }
      output.push(baseline);
    }
  }
  return output;
}

function extractCgiScript(html: string): string | null {
  const match = html.match(
    /<script[^>]*type=["']text\/javascript["'][^>]*h5only[^>]*>(?<code>[\s\S]*?window\.cgiDataNew\s*=\s*[\s\S]*?)<\/script>/i
  );
  return match?.groups?.code?.trim() || null;
}

const records = await capture();
const manifest = {
  schemaVersion: 1,
  generatedBy: 'test/baseline/capture.ts',
  sampleCount: records.length,
  groups: Object.fromEntries(samples.map(group => [group.name, group.samples.length])),
  records,
};

const serialized = `${JSON.stringify(manifest, null, 2)}\n`;
if (verifyMode) {
  const committed = fs.readFileSync(outputPath, 'utf8');
  if (committed !== serialized) {
    throw new Error(
      `${path.relative(repositoryRoot, outputPath)} is stale; run yarn test:baseline:capture after approving fixture changes`
    );
  }
  console.log(`verified ${path.relative(repositoryRoot, outputPath)} with ${records.length} records`);
} else {
  fs.writeFileSync(outputPath, serialized, 'utf8');
  console.log(`wrote ${path.relative(repositoryRoot, outputPath)} with ${records.length} records`);
}
