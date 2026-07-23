import { readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const generatedFiles = [
  'src/styles/theme.css',
  'src/styles/wechat-article-workspace.js',
  'src/styles/wechat-article-workspace.d.ts',
]
const generatedTimestamp = /^ \* Generated: .+\r?\n/m

for (const file of generatedFiles) {
  const path = resolve(file)
  const contents = await readFile(path, 'utf8')
  const matches = contents.match(new RegExp(generatedTimestamp.source, 'gm')) ?? []

  if (matches.length !== 1) {
    throw new Error(`expected exactly one generated timestamp in ${file}, found ${matches.length}`)
  }

  await writeFile(path, contents.replace(generatedTimestamp, ''), 'utf8')
}
