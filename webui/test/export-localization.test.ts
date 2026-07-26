import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { en } from '../src/i18n/messages.en'
import { zhCN } from '../src/i18n/messages.zh-CN'

const exportPageSource = readFileSync(new URL('../src/features/exports/ExportPage.tsx', import.meta.url), 'utf8')

describe('export localization and safe presentation', () => {
  it('provides every staged-flow label in both catalogs', () => {
    for (const catalog of [en.exports.workflow, zhCN.exports.workflow]) {
      expect(catalog.stages).not.toBe('')
      expect(catalog.scope).not.toBe('')
      expect(catalog.format).not.toBe('')
      expect(catalog.destination).not.toBe('')
      expect(catalog.continueToFormat).not.toBe('')
      expect(catalog.continueToDestination).not.toBe('')
      expect(catalog.recordLabel('markdown')).not.toBe('')
      expect(catalog.queued(catalog.jobLabel, 'job-ab…cdef')).not.toContain('job-export-fixture')
    }
  })

  it('provides localized account selector guidance in both catalogs', () => {
    for (const catalog of [en.articles.ux, zhCN.articles.ux]) {
      expect(catalog.accountDescription).not.toBe('')
      expect(catalog.albumDescription).not.toBe('')
    }
  })

  it('keeps raw selector keys and complete IDs out of normal-flow presentation', () => {
    expect(exportPageSource).not.toContain('stageCopy(')
    expect(exportPageSource).not.toContain("'Saved account'")
    expect(exportPageSource).not.toContain("'Saved album'")
    expect(exportPageSource).not.toContain('formatQuerySummary(')
    expect(exportPageSource).toContain('describeArticleQuery(')
    expect(exportPageSource).toContain('TechnicalDetails')
    expect(exportPageSource).toContain('handoffCreatedJob({ id: result.jobId })')
    expect(exportPageSource).not.toContain('setTimeout(() => handoffCreatedJob')
  })

  it('keeps manifest paths and raw verification payloads inside technical details', () => {
    expect(exportPageSource).toContain('safeFileName(file.path)')
    expect(exportPageSource).toContain('value: file.path')
    expect(exportPageSource).toContain('value: serializeVerificationIssue(issue)')
    expect(exportPageSource).toContain('return JSON.stringify(detail)')
    expect(exportPageSource).toContain('verificationIssue(index + 1)')
    expect(exportPageSource).not.toContain('issue.message?.trim()')
    expect(exportPageSource).not.toContain('<code>{file.path}</code>')
    expect(exportPageSource).not.toContain('issue.message ?? JSON.stringify(issue)')
  })
})
