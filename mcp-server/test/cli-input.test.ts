import assert from 'node:assert/strict';
import { Readable } from 'node:stream';
import test from 'node:test';
import { loadCliJsonObject, parseJsonObject } from '../src/cli/input.ts';

test('CLI accepts one JSON object source and rejects ambiguous or non-object input', async () => {
  assert.deepEqual(parseJsonObject('{"url":"https://mp.weixin.qq.com/s/example"}'), {
    url: 'https://mp.weixin.qq.com/s/example',
  });
  assert.throws(() => parseJsonObject('[]'), /must be an object/);
  assert.throws(() => parseJsonObject('{'), /Invalid JSON/);
  await assert.rejects(
    loadCliJsonObject({ input: '{}', stdin: true }, Readable.from(['{}'])),
    /exactly one JSON input source/
  );
  assert.deepEqual(await loadCliJsonObject({ stdin: true }, Readable.from(['{"fakeid":', '"account-1"}'])), {
    fakeid: 'account-1',
  });
});
