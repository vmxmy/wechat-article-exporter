import { readFile } from 'node:fs/promises';
import type { Readable } from 'node:stream';

export interface CliJsonInputOptions {
  input?: string;
  file?: string;
  stdin?: boolean;
}

export async function loadCliJsonObject(
  options: CliJsonInputOptions,
  stdin: Readable = process.stdin
): Promise<Record<string, unknown>> {
  const sourceCount = [options.input !== undefined, options.file !== undefined, options.stdin === true].filter(
    Boolean
  ).length;
  if (sourceCount > 1) throw new Error('Use exactly one JSON input source: --input, --file, or --stdin.');

  if (options.input !== undefined) return parseJsonObject(options.input, '--input');
  if (options.file !== undefined) return parseJsonObject(await readFile(options.file, 'utf8'), `file ${options.file}`);
  if (options.stdin === true) {
    let text = '';
    for await (const chunk of stdin) text += chunk.toString();
    return parseJsonObject(text, 'stdin');
  }
  return {};
}

export function parseJsonObject(text: string, source = 'input'): Record<string, unknown> {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    throw new Error(`Invalid JSON from ${source}.`);
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`JSON from ${source} must be an object.`);
  }
  return value as Record<string, unknown>;
}
