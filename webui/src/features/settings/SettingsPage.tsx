import { Banner } from '@astryxdesign/core/Banner'
import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { FileInput } from '@astryxdesign/core/FileInput'
import { Link } from '@astryxdesign/core/Link'
import { NumberInput } from '@astryxdesign/core/NumberInput'
import { Selector } from '@astryxdesign/core/Selector'
import { Section } from '@astryxdesign/core/Section'
import { TextInput } from '@astryxdesign/core/TextInput'
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { PageHeader, PageStack, Status, TechnicalDetails } from '../../components/presentation'
import { isLocale, messages as localeMessages, persistLocale, type Locale, type MessageCatalog } from '../../i18n'
import { formatBytes, formatCount, formatDateTime, formatHash, formatStatus } from '../../lib/presentation'
import { getBackupArtifactDownloadURL, getDiagnosticBundleDownloadURL, getProxyDisclosure, type BackupReceipt, type CredentialImportInput, type CredentialValidation, type DiagnosticBundleReceipt, type GarbageCollectionPlan, type Preferences, type ProxyInput, type ProxyRequestClass, type ProxyTrust, type RestoreCompletion, type RestoreConflictPolicy, type RestorePreparation } from '../../lib/api'
import { isPreferencesDirty, navigationGuard, reconcileLoadedPreferences, reconcileSavedPreferences } from '../../lib/navigationGuard'
import { useCredentials, useDiagnostics, useIntegrity, usePreferences, useProxies, useWorkspaceMutations } from '../../lib/queries'
import './settings.css'

const proxyClasses: readonly ProxyRequestClass[] = ['public_content', 'public_resource', 'management_session', 'article_credential', 'engagement_metrics', 'comments', 'paid_content']

const settingsSectionIDs = ['settings-general', 'settings-download-export', 'settings-credentials', 'settings-network', 'settings-storage', 'settings-diagnostics'] as const

type SettingsSectionID = typeof settingsSectionIDs[number]

export function SettingsPage({ locale, messages }: { readonly locale: Locale; readonly messages: MessageCatalog }) {
  const copy = messages.settings
  const credentials = useCredentials()
  const proxies = useProxies()
  const preferences = usePreferences()
  const integrity = useIntegrity()
  const diagnostics = useDiagnostics()
  const mutations = useWorkspaceMutations()
  const [notice, setNotice] = useState<string>()
  const [backup, setBackup] = useState<BackupReceipt>()
  const [backupID, setBackupID] = useState('')
  const [backupDownloadURL, setBackupDownloadURL] = useState<string>()
  const [plan, setPlan] = useState<GarbageCollectionPlan>()
  const [confirmation, setConfirmation] = useState('')
  const [diagnosticBundle, setDiagnosticBundle] = useState<DiagnosticBundleReceipt>()
  const [diagnosticBundleError, setDiagnosticBundleError] = useState<string>()

  const error = [credentials, proxies, preferences, integrity, diagnostics].find((query) => query.isError)
  const refreshing = () => {
    void credentials.refetch()
    void proxies.refetch()
    void preferences.refetch()
    void integrity.refetch()
    void diagnostics.refetch()
  }
  const failure = (reason: unknown) => setNotice(reason instanceof Error ? reason.message : copy.actionFailed)
  const savePreferences = async (value: Preferences) => {
    try {
      const saved = await mutations.patchPreferences.mutateAsync({ download: value.download, export: value.export, display: value.display, proxy: value.proxy })
      const language = isLocale(saved.display?.language) ? saved.display.language : value.display?.language
      if (isLocale(language)) {
        persistLocale(language)
        setNotice(localeMessages[language].settings.preferences.saved)
      } else {
        setNotice(copy.preferences.saved)
      }
      return saved
    } catch (reason) {
      failure(reason)
      throw reason
    }
  }

  return (
    <PageStack className="settings-page" aria-labelledby="settings-title">
      <PageHeader eyebrow={messages.navigation.operations} title={copy.title} titleId="settings-title" description={copy.description} />
      {error ? <Banner status="error" title={copy.unavailable} endContent={<Button label={copy.retry} variant="secondary" onClick={refreshing} />} /> : null}
      {notice ? <Banner status="success" title={notice} isDismissable onDismiss={() => setNotice(undefined)} /> : null}
      <PreferencesDraftProvider value={preferences.data} pending={mutations.patchPreferences.isPending} onSave={savePreferences}>
      <div className="settings-layout">
        <SettingsNavigation messages={messages} />
        <div className="settings-content">
          <SettingsSection id="settings-general" title={copy.navigation.general} description={copy.preferences.description}>
            <GeneralPreferencesPanel locale={locale} messages={messages} value={preferences.data} loading={preferences.isLoading} />
          </SettingsSection>
          <SettingsSection id="settings-download-export" title={copy.navigation.downloadExport} description={copy.preferences.exportDefaultsHint}>
            <DownloadExportDefaultsPanel locale={locale} messages={messages} value={preferences.data} loading={preferences.isLoading} />
          </SettingsSection>
          <SettingsSection id="settings-credentials" title={copy.credentials.title} description={copy.credentials.description}>
            <CredentialsPanel locale={locale} messages={messages} data={credentials.data} loading={credentials.isLoading} pending={mutations.importCredential.isPending || mutations.uploadCredentialFile.isPending || mutations.removeCredential.isPending} validationPending={mutations.validateCredential.isPending} onValidate={async (input) => {
              try {
                const result = await mutations.validateCredential.mutateAsync(input)
                setNotice(result.valid ? copy.credentials.validationPassed : copy.credentials.validationFailed)
                return result
              } catch (reason) {
                failure(reason)
                throw reason
              }
            }} onImport={(input) => mutations.importCredential.mutate(input, { onSuccess: () => setNotice(copy.credentials.imported), onError: failure })} onUpload={(file) => mutations.uploadCredentialFile.mutate(file, { onSuccess: () => setNotice(copy.credentials.fileImported), onError: failure })} onRemove={(id, removalConfirmation) => mutations.removeCredential.mutate({ id, confirmation: removalConfirmation }, { onSuccess: () => setNotice(copy.credentials.removed), onError: failure })} />
          </SettingsSection>
          <SettingsSection id="settings-network" title={copy.navigation.network} description={copy.proxies.description}>
            <NetworkPreferencesPanel locale={locale} messages={messages} value={preferences.data} loading={preferences.isLoading} />
            <ProxiesPanel locale={locale} messages={messages} data={proxies.data} loading={proxies.isLoading} pending={mutations.addProxy.isPending || mutations.removeProxy.isPending} onAdd={(input) => mutations.addProxy.mutate(input, { onSuccess: () => setNotice(copy.proxies.added), onError: failure })} onRemove={(id, removalConfirmation) => mutations.removeProxy.mutate({ id, confirmation: removalConfirmation }, { onSuccess: () => setNotice(copy.proxies.removed), onError: failure })} onToggle={(id, enabled) => mutations.setProxyEnabled.mutate({ id, enabled }, { onSuccess: () => setNotice(enabled ? copy.proxies.enabledNotice : copy.proxies.disabledNotice), onError: failure })} onTest={(id) => mutations.testProxy.mutate(id, { onSuccess: () => setNotice(copy.proxies.tested), onError: failure })} probe={mutations.testProxy.data} />
          </SettingsSection>
          <SettingsSection id="settings-storage" title={copy.navigation.storage} description={copy.storage.description}>
            <StorageMaintenancePanel locale={locale} messages={messages} backup={backup} backupID={backupID} backupDownloadURL={backupDownloadURL} plan={plan} confirmation={confirmation} mutations={mutations} integrity={integrity.data} integrityLoading={integrity.isLoading} onBackupIDChange={(next) => { setBackupID(next); setBackupDownloadURL(undefined) }} onCreateBackup={() => mutations.createBackup.mutate(undefined, {
              onSuccess: (result) => {
                setBackup(result)
                setBackupID(result.id)
                setBackupDownloadURL(getBackupArtifactDownloadURL(result.id))
                setNotice(copy.backups.created)
              },
              onError: failure
            })} onVerifyBackup={() => mutations.verifyBackup.mutate(backupID, { onError: failure })} onPlanGarbageCollection={() => mutations.planGarbageCollection.mutate(undefined, {
              onSuccess: (next) => {
                setPlan(next)
                setConfirmation('')
                setNotice(copy.gc.planned)
              },
              onError: failure
            })} onConfirmationChange={setConfirmation} onApplyGarbageCollection={() => mutations.applyGarbageCollection.mutate({ planId: plan?.id ?? '', confirmation }, {
              onSuccess: () => {
                setNotice(copy.gc.result)
                setPlan(undefined)
                setConfirmation('')
              },
              onError: (reason) => {
                setPlan(undefined)
                setConfirmation('')
                failure(reason)
              }
            })} onFailure={failure} />
          </SettingsSection>
          <SettingsSection id="settings-diagnostics" title={copy.diagnostics.title} description={copy.diagnostics.description}>
            <DiagnosticsPanel locale={locale} messages={messages} loading={diagnostics.isLoading} report={diagnostics.data} bundle={diagnosticBundle} bundleError={diagnosticBundleError} pending={mutations.createDiagnosticBundle.isPending} onCreateBundle={() => {
              setDiagnosticBundleError(undefined)
              mutations.createDiagnosticBundle.mutate(undefined, {
                onSuccess: (receipt) => {
                  setDiagnosticBundle(receipt)
                  setNotice(copy.diagnostics.bundleReady)
                },
                onError: (reason) => {
                  const message = reason instanceof Error ? reason.message : copy.actionFailed
                  setDiagnosticBundleError(message)
                  setNotice(undefined)
                }
              })
            }} />
          </SettingsSection>
        </div>
      </div>
      </PreferencesDraftProvider>
    </PageStack>
  )
}

function SettingsNavigation({ messages }: { readonly messages: MessageCatalog }) {
  const [activeID, setActiveID] = useState<SettingsSectionID>('settings-general')
  const preferences = useContext(PreferencesDraftContext)
  const sections = [
    { id: 'settings-general', label: messages.settings.navigation.general },
    { id: 'settings-download-export', label: messages.settings.navigation.downloadExport },
    { id: 'settings-credentials', label: messages.settings.navigation.credentials },
    { id: 'settings-network', label: messages.settings.navigation.network },
    { id: 'settings-storage', label: messages.settings.navigation.storage },
    { id: 'settings-diagnostics', label: messages.settings.navigation.diagnostics }
  ] as const

  useEffect(() => {
    const sectionElements = settingsSectionIDs.map((id) => document.getElementById(id)).filter((section): section is HTMLElement => section !== null)
    if (!sectionElements.length || !('IntersectionObserver' in window)) return
    const observer = new IntersectionObserver((entries) => {
      const current = entries
        .filter((entry) => entry.isIntersecting)
        .sort((left, right) => right.intersectionRatio - left.intersectionRatio)[0]
      if (current) setActiveID(current.target.id as SettingsSectionID)
    }, { rootMargin: '-12% 0px -68% 0px', threshold: [0.1, 0.4, 0.75] })
    sectionElements.forEach((section) => observer.observe(section))
    return () => observer.disconnect()
  }, [])

  return (
    <nav className="settings-secondary-nav" aria-label={messages.settings.navigation.label}>
      <p className="settings-secondary-nav-label">{messages.settings.navigation.label}</p>
      <ul>
        {sections.map((section) => (
          <li key={section.id}>
            <Link href={`#${section.id}`} isStandalone className="settings-secondary-nav-link" aria-current={activeID === section.id ? 'page' : undefined} onClick={() => setActiveID(section.id)}>{section.label}</Link>
          </li>
        ))}
      </ul>
      <Button label={messages.settings.preferences.save} variant="primary" isLoading={preferences?.pending} isDisabled={!preferences || preferences.pending} onClick={() => preferences?.save()} />
    </nav>
  )
}

function SettingsSection({ id, title, description, children }: { readonly id: SettingsSectionID; readonly title: string; readonly description: string; readonly children: ReactNode }) {
  return (
    <Section id={id} className="settings-section" variant="transparent" dividers={['bottom']} padding={0} aria-labelledby={`${id}-title`}>
      <header className="settings-section-header">
        <h2 id={`${id}-title`}>{title}</h2>
        <p>{description}</p>
      </header>
      <div className="settings-section-body">{children}</div>
    </Section>
  )
}

type RestoreMutations = ReturnType<typeof useWorkspaceMutations>

type PreferencesDraftContextValue = {
  readonly draft: Preferences
  readonly pending: boolean
  readonly update: (patch: Partial<Preferences>) => void
  readonly save: () => void
}

const PreferencesDraftContext = createContext<PreferencesDraftContextValue | undefined>(undefined)

function PreferencesDraftProvider({ value, pending, onSave, children }: { readonly value?: Preferences; readonly pending: boolean; readonly onSave: (value: Preferences) => Promise<Preferences>; readonly children: ReactNode }) {
  return value
    ? <PreferencesDraftEditor value={value} pending={pending} onSave={onSave}>{children}</PreferencesDraftEditor>
    : <>{children}</>
}

function PreferencesDraftEditor({ value, pending, onSave, children }: { readonly value: Preferences; readonly pending: boolean; readonly onSave: (value: Preferences) => Promise<Preferences>; readonly children: ReactNode }) {
  const [preferences, setPreferences] = useState({ draft: value, baseline: value })
  const dirty = isPreferencesDirty(preferences.draft, preferences.baseline)

  useEffect(() => {
    let cancelled = false
    queueMicrotask(() => {
      if (!cancelled) setPreferences((current) => reconcileLoadedPreferences(current.draft, current.baseline, value))
    })
    return () => { cancelled = true }
  }, [value])

  useEffect(() => {
    navigationGuard.setBlocker(() => dirty)
    const preventUnload = (event: BeforeUnloadEvent) => {
      if (!dirty) return
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', preventUnload)
    return () => {
      navigationGuard.setBlocker(undefined)
      window.removeEventListener('beforeunload', preventUnload)
    }
  }, [dirty])

  const context = useMemo<PreferencesDraftContextValue>(() => ({
    draft: preferences.draft,
    pending,
    update: (patch) => setPreferences((current) => ({ ...current, draft: { ...current.draft, ...patch } })),
    save: () => {
      if (pending) return
      const submitted = preferences.draft
      void onSave(submitted).then((saved) => {
        setPreferences((current) => reconcileSavedPreferences(current.draft, submitted, saved))
      }).catch(() => undefined)
    }
  }), [onSave, pending, preferences.draft])
  return <PreferencesDraftContext.Provider value={context}>{children}</PreferencesDraftContext.Provider>
}

function useRequiredPreferencesDraft() {
  const context = useContext(PreferencesDraftContext)
  if (!context) throw new Error('Preferences draft context is unavailable.')
  return context
}

function GeneralPreferencesPanel({ locale, messages, value, loading }: PreferencePanelProps) {
  const copy = messages.settings.preferences
  if (loading || !value) return <LoadingSettings messages={messages} />
  return <GeneralPreferencesEditor locale={locale} copy={copy} />
}

function GeneralPreferencesEditor({ locale, copy }: { readonly locale: Locale; readonly copy: MessageCatalog['settings']['preferences'] }) {
  const preferences = useRequiredPreferencesDraft()
  const language = isLocale(preferences.draft.display.language) ? preferences.draft.display.language : locale
  return <div className="settings-form settings-form-narrow"><Selector label={copy.language} options={[{ value: 'en', label: copy.languageEnglish }, { value: 'zh-CN', label: copy.languageChinese }]} value={language} onChange={(next) => preferences.update({ display: { ...preferences.draft.display, language: next as Locale } })} /></div>
}

function DownloadExportDefaultsPanel({ messages, value, loading }: PreferencePanelProps) {
  const copy = messages.settings.preferences
  if (loading || !value) return <LoadingSettings messages={messages} />
  return <DownloadExportDefaultsEditor copy={copy} />
}

function DownloadExportDefaultsEditor({ copy }: { readonly copy: MessageCatalog['settings']['preferences'] }) {
  const preferences = useRequiredPreferencesDraft()
  const { draft } = preferences
  return (
    <div className="settings-form">
      <div className="settings-field-group" aria-labelledby="download-defaults-title">
        <h3 id="download-defaults-title">{copy.downloadDefaults}</h3>
        <NumberInput label={copy.downloadConcurrency} value={draft.download.concurrency} onChange={(next) => preferences.update({ download: { ...draft.download, concurrency: next ?? 1 } })} min={1} step={1} isIntegerOnly />
        <CheckboxInput label={copy.forceContent} value={draft.download.forceContent} onChange={(next) => preferences.update({ download: { ...draft.download, forceContent: next } })} />
        <CheckboxInput label={copy.metadataOverrides} value={draft.download.metadataOverridesContent} onChange={(next) => preferences.update({ download: { ...draft.download, metadataOverridesContent: next } })} />
      </div>
      <div className="settings-field-group" aria-labelledby="export-defaults-title">
        <h3 id="export-defaults-title">{copy.exportDefaults}</h3>
        <TextInput label={copy.namingTemplate} value={draft.export.namingTemplate} onChange={(next) => preferences.update({ export: { ...draft.export, namingTemplate: next } })} />
        <NumberInput label={copy.maximumNameBytes} value={draft.export.maximumNameBytes} onChange={(next) => preferences.update({ export: { ...draft.export, maximumNameBytes: next ?? 1 } })} min={1} step={1} isIntegerOnly />
        <Selector label={copy.collisionPolicy} options={[{ value: 'fail', label: copy.collisionFail }, { value: 'skip', label: copy.collisionSkip }, { value: 'replace', label: copy.collisionReplace }, { value: 'suffix', label: copy.collisionSuffix }]} value={draft.export.collisionPolicy} onChange={(next) => preferences.update({ export: { ...draft.export, collisionPolicy: next } })} />
        <CheckboxInput label={copy.excelIncludeContent} value={draft.export.excelIncludeContent} onChange={(next) => preferences.update({ export: { ...draft.export, excelIncludeContent: next } })} />
        <CheckboxInput label={copy.jsonIncludeContent} value={draft.export.jsonIncludeContent} onChange={(next) => preferences.update({ export: { ...draft.export, jsonIncludeContent: next } })} />
        <CheckboxInput label={copy.jsonIncludeComments} value={draft.export.jsonIncludeComments} onChange={(next) => preferences.update({ export: { ...draft.export, jsonIncludeComments: next } })} />
        <CheckboxInput label={copy.htmlIncludeComments} value={draft.export.htmlIncludeComments} onChange={(next) => preferences.update({ export: { ...draft.export, htmlIncludeComments: next } })} />
      </div>
    </div>
  )
}

function NetworkPreferencesPanel({ messages, value, loading }: PreferencePanelProps) {
  const copy = messages.settings.preferences
  return value && !loading
    ? <NetworkPreferencesEditor copy={copy} />
    : <LoadingSettings messages={messages} />
}

function NetworkPreferencesEditor({ copy }: { readonly copy: MessageCatalog['settings']['preferences'] }) {
  const preferences = useRequiredPreferencesDraft()
  const { proxy } = preferences.draft
  return <div className="settings-form settings-form-narrow"><CheckboxInput label={copy.directFirst} value={proxy.directFirst} onChange={(next) => preferences.update({ proxy: { ...proxy, directFirst: next } })} /><CheckboxInput label={copy.fallbackEnabled} value={proxy.fallbackEnabled} onChange={(next) => preferences.update({ proxy: { ...proxy, fallbackEnabled: next } })} /></div>
}

type PreferencePanelProps = { readonly locale: Locale; readonly messages: MessageCatalog; readonly value?: Preferences; readonly loading: boolean }

function LoadingSettings({ messages }: { readonly messages: MessageCatalog }) {
  return <p className="settings-loading" role="status">{messages.settings.loading}</p>
}

function CredentialsPanel({ locale, messages, data, loading, pending, validationPending, onValidate, onImport, onUpload, onRemove }: { readonly locale: Locale; readonly messages: MessageCatalog; readonly data?: readonly CredentialDisplayMetadata[]; readonly loading: boolean; readonly pending: boolean; readonly validationPending: boolean; readonly onValidate: (input: CredentialImportInput) => Promise<CredentialValidation>; readonly onImport: (input: CredentialImportInput) => void; readonly onUpload: (file: File) => void; readonly onRemove: (id: string, confirmation: string) => void }) {
  const copy = messages.settings.credentials
  const [nickname, setNickname] = useState('')
  const [biz, setBiz] = useState('')
  const [uin, setUIN] = useState('')
  const [key, setKey] = useState('')
  const [passTicket, setPassTicket] = useState('')
  const [wapSid2, setWapSID2] = useState('')
  const [appMsgToken, setAppMsgToken] = useState('')
  const [cookie, setCookie] = useState('')
  const [validated, setValidated] = useState(false)
  const [removingID, setRemovingID] = useState<string>()
  const [removalConfirmation, setRemovalConfirmation] = useState('')
  const input = (): CredentialImportInput => ({ nickname: nickname || undefined, biz, uin, key, passTicket, wapSid2, appMsgToken, cookie: cookie || undefined })
  const changed = (set: (value: string) => void) => (value: string) => { set(value); setValidated(false) }
  const validate = () => { void onValidate(input()).then((result) => setValidated(result.valid)).catch(() => setValidated(false)) }
  const submit = () => {
    onImport(input())
    setNickname('')
    setBiz('')
    setUIN('')
    setKey('')
    setPassTicket('')
    setWapSID2('')
    setAppMsgToken('')
    setCookie('')
    setValidated(false)
  }
  const upload = async (file: File | File[] | null) => {
    if (file instanceof File) onUpload(file)
  }
  const cancelRemoval = () => { setRemovingID(undefined); setRemovalConfirmation('') }
  const remove = () => {
    if (!removingID) return
    onRemove(removingID, removalConfirmation)
    cancelRemoval()
  }

  return (
    <div className="settings-stack">
      <div className="settings-field-group" aria-labelledby="credential-import-title">
        <h3 id="credential-import-title">{copy.importTitle}</h3>
        <div className="settings-form">
          <TextInput label={copy.nickname} value={nickname} htmlName="credential-nickname" onChange={changed(setNickname)} isOptional />
          <TextInput label={copy.biz} value={biz} htmlName="credential-biz" onChange={changed(setBiz)} type="password" />
          <TextInput label={copy.uin} value={uin} htmlName="credential-uin" onChange={changed(setUIN)} type="password" />
          <TextInput label={copy.key} value={key} htmlName="credential-key" onChange={changed(setKey)} type="password" />
          <TextInput label={copy.passTicket} value={passTicket} htmlName="credential-pass-ticket" onChange={changed(setPassTicket)} type="password" />
          <TextInput label={copy.wapSid2} value={wapSid2} htmlName="credential-wap-sid2" onChange={changed(setWapSID2)} type="password" />
          <TextInput label={copy.appMsgToken} value={appMsgToken} htmlName="credential-appmsg-token" onChange={changed(setAppMsgToken)} type="password" />
          <TextInput label={copy.cookie} value={cookie} htmlName="credential-cookie" onChange={changed(setCookie)} type="password" isOptional />
          <p className="settings-hint">{copy.validationHint}</p>
          <div className="settings-action-row"><Button label={copy.validate} variant="secondary" isLoading={validationPending} isDisabled={pending || validationPending} onClick={validate} /><Button label={copy.import} variant="primary" isLoading={pending} isDisabled={!validated || validationPending} onClick={submit} /></div>
          <FileInput label={copy.file} value={null} onChange={(file) => { void upload(file) }} accept="application/json,.json" description={copy.fileHint} isDisabled={pending || validationPending} isLoading={pending || validationPending} />
        </div>
      </div>
      <div className="settings-field-group" aria-labelledby="credential-list-title">
        <h3 id="credential-list-title">{copy.listTitle}</h3>
        {loading ? <LoadingSettings messages={messages} /> : null}
        {data?.length ? <div className="data-table-wrap"><table className="data-table"><thead><tr><th>{copy.columns.account}</th><th>{copy.columns.kind}</th><th>{copy.columns.status}</th><th>{copy.columns.updated}</th><th><span className="sr-only">{copy.remove}</span></th></tr></thead><tbody>{data.map((item) => <tr key={item.id}><td><span>{credentialAccountLabel(item, copy)}</span><TechnicalDetails label={copy.technicalDetails} items={[{ label: copy.accountId, value: item.accountId, copyLabel: copy.copyAccountId }]} /></td><td>{humanizeIdentifier(item.kind, locale)}</td><td><Status value={item.status} locale={locale} /></td><td>{formatDateTime(item.updatedAt, locale)}</td><td><Button label={copy.remove} variant="secondary" size="sm" isDisabled={pending || validationPending} onClick={() => { setRemovingID(item.id); setRemovalConfirmation('') }} /></td></tr>)}</tbody></table></div> : !loading ? <p className="settings-empty">{copy.empty}</p> : null}
        {removingID ? <RemovalConfirmation copy={copy} expected={copy.removeConfirmation(removingID)} value={removalConfirmation} pending={pending} onChange={setRemovalConfirmation} onCancel={cancelRemoval} onRemove={remove} /> : null}
      </div>
    </div>
  )
}

type CredentialDisplayMetadata = {
  readonly id: string
  readonly accountId: string
  readonly accountName?: string
  readonly accountNameAvailable?: boolean
  readonly kind: string
  readonly status: string
  readonly updatedAt: string
}

function credentialAccountLabel(item: CredentialDisplayMetadata, copy: MessageCatalog['settings']['credentials']) {
  const accountName = item.accountName?.trim()
  return accountName || copy.accountUnavailable
}

function ProxiesPanel({ locale, messages, data, loading, pending, onAdd, onRemove, onToggle, onTest, probe }: { readonly locale: Locale; readonly messages: MessageCatalog; readonly data?: readonly ProxyDisplayRoute[]; readonly loading: boolean; readonly pending: boolean; readonly onAdd: (input: ProxyInput) => void; readonly onRemove: (id: string, confirmation: string) => void; readonly onToggle: (id: string, enabled: boolean) => void; readonly onTest: (id: string) => void; readonly probe?: { readonly route: { readonly id: string }; readonly responseValid: boolean; readonly credentialEligible: boolean; readonly errorClass?: string } }) {
  const copy = messages.settings.proxies
  const [name, setName] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [authorization, setAuthorization] = useState('')
  const [trust, setTrust] = useState<ProxyTrust>('public-only')
  const [priority, setPriority] = useState('0')
  const [classes, setClasses] = useState<readonly ProxyRequestClass[]>(['public_content', 'public_resource'])
  const [disclosure, setDisclosure] = useState<{ readonly required: boolean; readonly confirmation?: string; readonly secrets?: readonly string[] }>()
  const [confirm, setConfirm] = useState('')
  const [removingID, setRemovingID] = useState<string>()
  const [removalConfirmation, setRemovalConfirmation] = useState('')
  const proposal = useMemo<ProxyInput>(() => ({ name, endpoint, authorization: authorization || undefined, trust, classes, priority: Number(priority) || 0, confirm: trust === 'credential-trusted' ? confirm : undefined }), [authorization, classes, confirm, endpoint, name, priority, trust])
  const invalidateDisclosure = () => { setDisclosure(undefined); setConfirm('') }
  const disclose = () => {
    if (trust === 'credential-trusted' && name && endpoint) void getProxyDisclosure(proposal).then(setDisclosure).catch(() => setDisclosure(undefined))
  }
  const add = () => {
    if (trust === 'credential-trusted' && !disclosure?.confirmation) {
      disclose()
      return
    }
    onAdd(proposal)
    setAuthorization('')
  }
  const cancelRemoval = () => { setRemovingID(undefined); setRemovalConfirmation('') }
  const remove = () => {
    if (!removingID) return
    onRemove(removingID, removalConfirmation)
    cancelRemoval()
  }

  return (
    <div className="settings-stack settings-proxy-stack">
      <div className="settings-field-group" aria-labelledby="proxy-add-title">
        <h3 id="proxy-add-title">{copy.addTitle}</h3>
        <div className="settings-form settings-proxy-form">
          <TextInput label={copy.name} value={name} htmlName="proxy-name" onChange={(next) => { invalidateDisclosure(); setName(next) }} />
          <TextInput label={copy.endpoint} value={endpoint} htmlName="proxy-endpoint" onChange={(next) => { invalidateDisclosure(); setEndpoint(next) }} placeholder={copy.endpointPlaceholder} />
          <TextInput label={copy.authorization} value={authorization} htmlName="proxy-authorization" onChange={setAuthorization} type="password" isOptional />
          <Selector label={copy.trust} options={[{ value: 'public-only', label: copy.publicOnly }, { value: 'credential-trusted', label: copy.credentialTrusted }]} value={trust} htmlName="proxy-trust" onChange={(next) => { invalidateDisclosure(); setTrust(next as ProxyTrust) }} />
          <NumberInput label={copy.priority} value={Number(priority) || 0} htmlName="proxy-priority" onChange={(next) => { invalidateDisclosure(); setPriority(String(next ?? 0)) }} step={1} isIntegerOnly />
          <fieldset><legend>{copy.classes}</legend>{proxyClasses.map((item) => <CheckboxInput key={item} label={humanizeIdentifier(item, locale)} value={classes.includes(item)} onChange={(checked) => { invalidateDisclosure(); setClasses(checked ? [...classes, item] : classes.filter((value) => value !== item)) }} />)}</fieldset>
          <ProxyTrustExplanation trust={trust} copy={copy} />
          {trust === 'credential-trusted' ? <Button label={copy.disclosure} variant="secondary" onClick={disclose} /> : null}
          {disclosure?.required ? <div className="confirmation-proof"><span>{copy.disclosureRequired}{disclosure.secrets?.map((secret) => humanizeIdentifier(secret, locale)).join(', ')}</span><code translate="no">{disclosure.confirmation}</code><p>{copy.confirmationHint}</p><TextInput label={copy.confirmation} value={confirm} htmlName="proxy-confirmation" onChange={setConfirm} /></div> : null}
          <div className="settings-action-row"><Button label={copy.add} variant="primary" isLoading={pending} isDisabled={!name || !endpoint || (trust === 'credential-trusted' && (!disclosure?.confirmation || confirm !== disclosure.confirmation))} onClick={add} /></div>
        </div>
      </div>
      <div className="settings-field-group" aria-labelledby="proxy-list-title">
        <h3 id="proxy-list-title">{copy.listTitle}</h3>
        {loading ? <LoadingSettings messages={messages} /> : null}
        {data?.length ? <div className="data-table-wrap"><table className="data-table"><thead><tr><th>{copy.columns.name}</th><th>{copy.columns.endpoint}</th><th>{copy.columns.trust}</th><th>{copy.columns.priority}</th><th>{copy.columns.health}</th><th>{copy.columns.state}</th><th>{copy.columns.actions}</th></tr></thead><tbody>{data.map((route) => <tr key={route.id}><td>{route.name}</td><td><code className="settings-code" translate="no">{route.endpoint}</code></td><td><div className="settings-trust-cell"><span>{proxyTrustLabel(route.trust, copy)}</span><small>{proxyTrustSummary(route.trust, copy)}</small></div></td><td>{formatCount(route.priority, locale)}</td><td><Status value={route.health.state} locale={locale} /></td><td><Status value={route.enabled ? 'enabled' : 'disabled'} locale={locale} /></td><td><div className="action-button-group"><Button label={route.enabled ? copy.disable : copy.enable} variant="secondary" size="sm" onClick={() => onToggle(route.id, !route.enabled)} /><Button label={copy.test} variant="secondary" size="sm" onClick={() => onTest(route.id)} /><Button label={copy.remove} variant="secondary" size="sm" isDisabled={pending} onClick={() => { setRemovingID(route.id); setRemovalConfirmation('') }} /></div>{probe?.route.id === route.id ? <small className="settings-probe-result">{copy.probe}: <Status value={probe.responseValid ? 'valid' : 'invalid'} locale={locale} />{probe.errorClass ? ` · ${humanizeIdentifier(probe.errorClass, locale)}` : ''}</small> : null}</td></tr>)}</tbody></table></div> : !loading ? <p className="settings-empty">{copy.empty}</p> : null}
        {removingID ? <RemovalConfirmation copy={copy} expected={copy.removeConfirmation(removingID)} value={removalConfirmation} pending={pending} onChange={setRemovalConfirmation} onCancel={cancelRemoval} onRemove={remove} /> : null}
      </div>
    </div>
  )
}

type ProxyDisplayRoute = {
  readonly id: string
  readonly name: string
  readonly endpoint: string
  readonly trust: ProxyTrust
  readonly priority: number
  readonly enabled: boolean
  readonly health: { readonly state: string }
}

function ProxyTrustExplanation({ trust, copy }: { readonly trust: ProxyTrust; readonly copy: MessageCatalog['settings']['proxies'] }) {
  return <p className="settings-trust-explanation"><strong>{proxyTrustLabel(trust, copy)}.</strong> {proxyTrustSummary(trust, copy)}</p>
}

function proxyTrustLabel(trust: ProxyTrust, copy?: MessageCatalog['settings']['proxies']) {
  if (trust === 'credential-trusted') return copy?.credentialTrusted ?? trust
  return copy?.publicOnly ?? trust
}

function proxyTrustSummary(trust: ProxyTrust, copy: MessageCatalog['settings']['proxies']) {
  return trust === 'credential-trusted' ? copy.credentialTrustedExplanation : copy.publicOnlyExplanation
}

type RemovalConfirmationCopy = { readonly removeConfirmationLabel: string; readonly removeConfirmationHint: string; readonly confirmRemove: string; readonly cancelRemove: string }

function RemovalConfirmation({ copy, expected, value, pending, onChange, onCancel, onRemove }: { readonly copy: RemovalConfirmationCopy; readonly expected: string; readonly value: string; readonly pending: boolean; readonly onChange: (value: string) => void; readonly onCancel: () => void; readonly onRemove: () => void }) {
  return <section className="confirmation-proof" aria-label={copy.removeConfirmationLabel}><strong>{copy.removeConfirmationLabel}</strong><code translate="no">{expected}</code><p>{copy.removeConfirmationHint}</p><TextInput label={copy.removeConfirmationLabel} value={value} htmlName="removal-confirmation" onChange={onChange} /><div className="settings-action-row"><Button label={copy.confirmRemove} variant="destructive" isLoading={pending} isDisabled={value !== expected} onClick={onRemove} /><Button label={copy.cancelRemove} variant="secondary" isDisabled={pending} onClick={onCancel} /></div></section>
}

function StorageMaintenancePanel({ locale, messages, backup, backupID, backupDownloadURL, plan, confirmation, mutations, integrity, integrityLoading, onBackupIDChange, onCreateBackup, onVerifyBackup, onPlanGarbageCollection, onConfirmationChange, onApplyGarbageCollection, onFailure }: { readonly locale: Locale; readonly messages: MessageCatalog; readonly backup?: BackupReceipt; readonly backupID: string; readonly backupDownloadURL?: string; readonly plan?: GarbageCollectionPlan; readonly confirmation: string; readonly mutations: RestoreMutations; readonly integrity?: { readonly checkedAt: string; readonly issues: readonly { readonly kind: string; readonly message: string; readonly repairable: boolean; readonly recommendation?: string }[] }; readonly integrityLoading: boolean; readonly onBackupIDChange: (value: string) => void; readonly onCreateBackup: () => void; readonly onVerifyBackup: () => void; readonly onPlanGarbageCollection: () => void; readonly onConfirmationChange: (value: string) => void; readonly onApplyGarbageCollection: () => void; readonly onFailure: (reason: unknown) => void }) {
  const copy = messages.settings
  return (
    <div className="settings-stack">
      <div className="settings-field-group" aria-labelledby="backup-title">
        <h3 id="backup-title">{copy.backups.title}</h3>
        <p className="settings-hint">{copy.backups.description}</p>
        <div className="settings-form settings-form-narrow">
          <div className="settings-action-row"><Button label={copy.backups.create} variant="primary" isLoading={mutations.createBackup.isPending} onClick={onCreateBackup} /></div>
          {backup ? <BackupSummary locale={locale} messages={messages} backup={backup} /> : null}
          {backupID ? <TextInput label={copy.backups.backupId} value={backupID} onChange={onBackupIDChange} /> : null}
          {backupDownloadURL ? <a className="artifact-download" href={backupDownloadURL} onClick={() => undefined}>{copy.backups.download}</a> : null}
          <div className="settings-action-row"><Button label={copy.backups.verify} variant="secondary" isLoading={mutations.verifyBackup.isPending} isDisabled={!backupID} onClick={onVerifyBackup} /></div>
          {mutations.verifyBackup.data ? <Status value={mutations.verifyBackup.data.valid ? 'valid' : 'invalid'} locale={locale} label={mutations.verifyBackup.data.valid ? copy.backups.valid : copy.backups.invalid} /> : null}
        </div>
      </div>
      <IntegrityPanel locale={locale} messages={messages} loading={integrityLoading} report={integrity} />
      <section className="settings-danger-zone" aria-labelledby="danger-zone-title">
        <header><p className="settings-danger-eyebrow">{copy.storage.dangerEyebrow}</p><h3 id="danger-zone-title">{copy.storage.dangerTitle}</h3><p>{copy.storage.dangerDescription}</p></header>
        <RestorePanel messages={messages} mutations={mutations} onFailure={onFailure} />
        <section className="settings-danger-action" aria-labelledby="gc-title"><h4 id="gc-title">{copy.gc.title}</h4><p>{copy.gc.description}</p><div className="settings-form"><Button label={copy.gc.plan} variant="secondary" isLoading={mutations.planGarbageCollection.isPending} onClick={onPlanGarbageCollection} />{plan ? <GCPlan locale={locale} messages={messages} plan={plan} confirmation={confirmation} onConfirmation={onConfirmationChange} onApply={onApplyGarbageCollection} pending={mutations.applyGarbageCollection.isPending} /> : null}</div></section>
      </section>
    </div>
  )
}

function BackupSummary({ locale, messages, backup }: { readonly locale: Locale; readonly messages: MessageCatalog; readonly backup: BackupReceipt }) {
  const copy = messages.settings.backups
  return <div className="settings-summary"><dl><div><dt>{copy.summaryCreated}</dt><dd>{formatDateTime(backup.createdAt, locale)}</dd></div><div><dt>{copy.summarySize}</dt><dd>{formatBytes(backup.bytes, locale)}</dd></div><div><dt>{copy.summaryObjects}</dt><dd>{formatCount(backup.objects, locale)}</dd></div></dl><TechnicalDetails label={copy.technicalDetails} items={[{ label: copy.checksum, value: backup.sha256, copyLabel: copy.copyChecksum }]} /></div>
}

function RestorePanel({ messages, mutations, onFailure }: { readonly messages: MessageCatalog; readonly mutations: RestoreMutations; readonly onFailure: (reason: unknown) => void }) {
  const copy = messages.settings.backups
  const [archive, setArchive] = useState<File>()
  const [policy, setPolicy] = useState<RestoreConflictPolicy>('refuse')
  const [preparation, setPreparation] = useState<RestorePreparation>()
  const [confirmation, setConfirmation] = useState('')
  const [completed, setCompleted] = useState<RestoreCompletion>()
  const staging = mutations.uploadRestoreArchive.isPending || mutations.prepareRestore.isPending
  const stage = () => {
    if (!archive) return
    setPreparation(undefined)
    setConfirmation('')
    const stagedArchive = archive
    const stagedPolicy = policy
    mutations.uploadRestoreArchive.mutate(stagedArchive, {
      onSuccess: (upload) => mutations.prepareRestore.mutate({ uploadHandle: upload.handle, conflictPolicy: stagedPolicy }, {
        onSuccess: (next) => { if (archive === stagedArchive && policy === stagedPolicy) setPreparation(next) },
        onError: onFailure
      }),
      onError: onFailure
    })
  }
  const commit = () => {
    if (!preparation) return
    mutations.commitRestore.mutate({ preparationId: preparation.id, confirmation }, { onSuccess: setCompleted, onError: onFailure })
  }
  if (completed) return <div className="confirmation-proof" role="status" aria-live="assertive"><h4>{copy.terminalTitle}</h4><p>{copy.terminalMessage}</p></div>
  return <section className="settings-danger-action" aria-labelledby="restore-title"><h4 id="restore-title">{copy.restoreTitle}</h4><p>{copy.restoreDescription}</p><Banner status="warning" title={copy.destructiveWarning} /><div className="settings-form"><FileInput label={copy.archive} value={archive ?? null} accept=".wab,.zip,application/octet-stream,application/zip" isDisabled={staging} isLoading={staging} onChange={(file) => { setArchive(file instanceof File ? file : undefined); setPreparation(undefined); setConfirmation('') }} /><Selector label={copy.policy} options={[{ value: 'refuse', label: copy.refuse }, { value: 'rename', label: copy.rename }]} value={policy} isDisabled={staging} onChange={(next) => { setPolicy(next as RestoreConflictPolicy); setPreparation(undefined); setConfirmation('') }} /><div className="settings-action-row"><Button label={staging ? copy.staging : copy.stage} variant="secondary" isLoading={staging} isDisabled={!archive || staging} onClick={stage} /></div>{preparation ? <div className="confirmation-proof"><code>{preparation.confirmation}</code><p>{copy.confirmationHint}</p><TextInput label={copy.confirmation} value={confirmation} onChange={setConfirmation} /><Button label={copy.commit} variant="destructive" isLoading={mutations.commitRestore.isPending} isDisabled={confirmation !== preparation.confirmation} onClick={commit} /></div> : null}</div></section>
}

function IntegrityPanel({ locale, messages, loading, report }: { readonly locale: Locale; readonly messages: MessageCatalog; readonly loading: boolean; readonly report?: { readonly checkedAt: string; readonly issues: readonly { readonly kind: string; readonly message: string; readonly repairable: boolean; readonly recommendation?: string }[] } }) {
  const copy = messages.settings.integrity
  return <section className="settings-field-group" aria-labelledby="integrity-title"><h3 id="integrity-title">{copy.title}</h3><p className="settings-hint">{copy.description}</p>{loading ? <LoadingSettings messages={messages} /> : null}{report ? <><p className="settings-summary-line">{copy.checked}: {formatDateTime(report.checkedAt, locale)} · {formatCount(report.issues.length, locale)} {copy.issues}</p>{report.issues.length ? <div className="data-table-wrap"><table className="data-table"><thead><tr><th>{copy.columns.kind}</th><th>{copy.columns.message}</th><th>{copy.columns.repairable}</th><th>{copy.columns.recommendation}</th></tr></thead><tbody>{report.issues.map((item, index) => <tr key={`${item.kind}-${index}`}><td>{humanizeIdentifier(item.kind, locale)}</td><td>{item.message}</td><td>{item.repairable ? messages.settings.common.yes : messages.settings.common.no}</td><td>{item.recommendation ?? '—'}</td></tr>)}</tbody></table></div> : <p className="settings-empty">{copy.noIssues}</p>}</> : null}</section>
}

function GCPlan({ locale, messages, plan, confirmation, onConfirmation, onApply, pending }: { readonly locale: Locale; readonly messages: MessageCatalog; readonly plan: GarbageCollectionPlan; readonly confirmation: string; readonly onConfirmation: (value: string) => void; readonly onApply: () => void; readonly pending: boolean }) {
  const copy = messages.settings.gc
  const items = [[copy.categories.objects, plan.unreferencedObjects], [copy.categories.temporary, plan.temporaryFiles], [copy.categories.debug, plan.expiredDebugCaptures], [copy.categories.logs, plan.completedJobLogs]] as const
  return <div className="confirmation-proof"><strong>{copy.totals}</strong><span>{copy.generated}: {formatDateTime(plan.generatedAt, locale)}</span><span>{copy.expires}: {plan.expiresAt ? formatDateTime(plan.expiresAt, locale) : '—'}</span><ul>{items.map(([name, value]) => <li key={name}>{name}: {formatQuantity(value.count, value.bytes, locale, copy)}</li>)}</ul><code>{plan.confirmation}</code><TextInput label={copy.confirmation} value={confirmation} onChange={onConfirmation} /><Button label={copy.apply} variant="destructive" isLoading={pending} isDisabled={confirmation !== plan.confirmation} onClick={onApply} /></div>
}

function DiagnosticsPanel({ locale, messages, loading, report, bundle, bundleError, pending, onCreateBundle }: { readonly locale: Locale; readonly messages: MessageCatalog; readonly loading: boolean; readonly report?: { readonly collectedAt: string; readonly checks: readonly { readonly name: string; readonly status: string; readonly summary?: string }[] }; readonly bundle?: DiagnosticBundleReceipt; readonly bundleError?: string; readonly pending: boolean; readonly onCreateBundle: () => void }) {
  const copy = messages.settings.diagnostics
  return <div className="settings-stack"><div className="settings-form settings-form-narrow"><div className="settings-action-row"><Button label={pending ? copy.creatingBundle : copy.createBundle} variant="secondary" isLoading={pending} onClick={onCreateBundle} /></div>{bundleError ? <Banner status="error" title={bundleError} /> : null}{bundle ? <DiagnosticBundleSummary locale={locale} messages={messages} bundle={bundle} /> : null}</div>{loading ? <LoadingSettings messages={messages} /> : null}{report ? <><p className="settings-summary-line">{copy.collected}: {formatDateTime(report.collectedAt, locale)}</p>{report.checks.length ? <div className="data-table-wrap"><table className="data-table"><thead><tr><th>{copy.columns.check}</th><th>{copy.columns.status}</th><th>{copy.columns.summary}</th></tr></thead><tbody>{report.checks.map((item) => <tr key={item.name}><td>{humanizeIdentifier(item.name, locale)}</td><td><Status value={item.status} locale={locale} /></td><td>{item.summary ?? '—'}</td></tr>)}</tbody></table></div> : <p className="settings-empty">{copy.empty}</p>}</> : null}</div>
}

function DiagnosticBundleSummary({ locale, messages, bundle }: { readonly locale: Locale; readonly messages: MessageCatalog; readonly bundle: DiagnosticBundleReceipt }) {
  const copy = messages.settings.diagnostics
  return <div className="settings-summary"><p>{copy.bundleReady}</p><p>{copy.bundleDescription}</p><dl><div><dt>{copy.summaryCreated}</dt><dd>{formatDateTime(bundle.createdAt, locale)}</dd></div><div><dt>{copy.bundleExpires}</dt><dd>{formatDateTime(bundle.expiresAt, locale)}</dd></div><div><dt>{copy.summarySize}</dt><dd>{formatBytes(bundle.sizeBytes, locale)}</dd></div><div><dt>{copy.bundleChecksum}</dt><dd><code title={bundle.sha256}>{formatHash(bundle.sha256)}</code></dd></div></dl><TechnicalDetails label={copy.technicalDetails} items={[{ label: copy.checksum, value: bundle.sha256, copyLabel: copy.copyChecksum }]} /><a className="artifact-download" href={getDiagnosticBundleDownloadURL(bundle.handle)}>{copy.downloadBundle}</a></div>
}

function formatQuantity(count: number, bytes: number, locale: Locale, copy: MessageCatalog['settings']['gc']) {
  return copy.quantity(formatCount(count, locale), formatBytes(bytes, locale))
}

function humanizeIdentifier(value: string, locale: Locale) {
  const status = formatStatus(value, locale)
  if (status.isKnown) return status.label
  return identifierLabels[value.trim().toLowerCase()]?.[locale] ?? '—'
}

const identifierLabels: Readonly<Record<string, Readonly<Record<Locale, string>>>> = {
  'wechat-article': { en: 'WeChat article credential', 'zh-CN': '微信公众号文章凭据' },
  wechat: { en: 'WeChat credential', 'zh-CN': '微信凭据' },
  cookie: { en: 'Cookie credential', 'zh-CN': 'Cookie 凭据' },
  public_content: { en: 'Public article content', 'zh-CN': '公开文章内容' },
  public_resource: { en: 'Public article resources', 'zh-CN': '公开文章资源' },
  management_session: { en: 'Account management session', 'zh-CN': '账号管理会话' },
  article_credential: { en: 'Credential-protected article', 'zh-CN': '凭据保护的文章' },
  engagement_metrics: { en: 'Engagement metrics', 'zh-CN': '互动数据' },
  comments: { en: 'Comments', 'zh-CN': '评论' },
  paid_content: { en: 'Paid content', 'zh-CN': '付费内容' },
  appmsg_token: { en: 'Article access token', 'zh-CN': '文章访问令牌' },
  runtime: { en: 'Local runtime', 'zh-CN': '本地运行环境' },
  session: { en: 'WeChat session', 'zh-CN': '微信会话' },
  storage: { en: 'Local storage', 'zh-CN': '本地存储' },
  loopback: { en: 'Local connection', 'zh-CN': '本地连接' },
  sqlite: { en: 'Database integrity', 'zh-CN': '数据库完整性' },
  'foreign-key': { en: 'Database relationship', 'zh-CN': '数据库关联' },
  'migration-state': { en: 'Database version', 'zh-CN': '数据库版本' },
  'missing-object': { en: 'Missing stored object', 'zh-CN': '存储对象缺失' },
  'corrupt-object': { en: 'Damaged stored object', 'zh-CN': '存储对象损坏' },
  network: { en: 'Network error', 'zh-CN': '网络错误' },
  timeout: { en: 'Connection timed out', 'zh-CN': '连接超时' },
  policy: { en: 'Blocked by network policy', 'zh-CN': '被网络策略阻止' }
}
