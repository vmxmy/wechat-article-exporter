import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
const cwd = path.resolve(import.meta.dirname, '..');
const cli = path.join(cwd, 'src/cli/wechat-article.ts');

test('CLI help documents the stable API gateway and structured input sources', async () => {
  const result = await execFileAsync(process.execPath, ['--experimental-strip-types', cli, 'help'], { cwd });
  assert.match(result.stdout, /wechat-article api list/);
  assert.match(result.stdout, /api describe <tool>/);
  assert.match(result.stdout, /--input <json> \| --file <path> \| --stdin/);
});

test('CLI dry-run returns redacted JSON without requiring a server connection', async () => {
  const result = await execFileAsync(
    process.execPath,
    [
      '--experimental-strip-types',
      cli,
      'api',
      'call',
      'download_article',
      '--input',
      '{"url":"https://mp.weixin.qq.com/s/example","auth_key":"secret"}',
      '--dry-run',
    ],
    { cwd }
  );
  const output = JSON.parse(result.stdout);
  assert.equal(output.dryRun, true);
  assert.equal(output.arguments.auth_key, '[REDACTED]');
  assert.equal(result.stdout.includes('secret'), false);
});

test('CLI uses exit code 2 for ambiguous input usage errors', async () => {
  await assert.rejects(
    execFileAsync(
      process.execPath,
      ['--experimental-strip-types', cli, 'api', 'call', 'download_article', '--input', '{}', '--stdin'],
      { cwd, input: '{}' }
    ),
    error => {
      const failure = error as Error & { code?: number; stderr?: string };
      assert.equal(failure.code, 2);
      assert.match(failure.stderr ?? '', /exactly one JSON input source/);
      return true;
    }
  );
});

test('CLI rejects server URLs containing embedded credentials', async () => {
  await assert.rejects(
    execFileAsync(
      process.execPath,
      ['--experimental-strip-types', cli, 'status', '--server', 'https://user:password@example.com'],
      { cwd }
    ),
    error => {
      const failure = error as Error & { code?: number; stderr?: string };
      assert.equal(failure.code, 2);
      assert.match(failure.stderr ?? '', /must not contain credentials/);
      assert.equal((failure.stderr ?? '').includes('password'), false);
      return true;
    }
  );
});
