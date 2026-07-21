import assert from 'node:assert/strict';
import test from 'node:test';
import { assertToolConfirmation, createToolDryRun, requiredToolConfirmation } from '../src/cli/safety.ts';

test('known read tools are safe while unknown write-capable tools require exact confirmation', () => {
  const known = { name: 'download_article', inputSchema: { type: 'object' as const } };
  assert.equal(requiredToolConfirmation(known), null);

  const future = { name: 'delete_export_cache', inputSchema: { type: 'object' as const } };
  assert.equal(requiredToolConfirmation(future), 'delete_export_cache');
  assert.throws(() => assertToolConfirmation(future), /--confirm delete_export_cache/);
  assert.doesNotThrow(() => assertToolConfirmation(future, 'delete_export_cache'));
});

test('dry-run redacts credentials and reports that no MCP call was made', () => {
  const preview = createToolDryRun('download_article', {
    url: 'https://mp.weixin.qq.com/s/example',
    access_token: 'secret-token',
    nested: { authKey: 'secret-key', format: 'markdown' },
  });
  assert.deepEqual(preview.arguments, {
    url: 'https://mp.weixin.qq.com/s/example',
    access_token: '[REDACTED]',
    nested: { authKey: '[REDACTED]', format: 'markdown' },
  });
  assert.equal(preview.dryRun, true);
  assert.match(String(preview.note), /No MCP connection/);
});
