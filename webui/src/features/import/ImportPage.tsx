import { Button } from '@/components/controls/Button'
import { CheckboxInput } from '@/components/controls/CheckboxInput'
import { TextInput } from '@/components/controls/TextInput'
import { Toolbar } from '@/components/controls/Toolbar'
import { useState } from 'react'
import { ActionGroup, PageHeader, PageStack } from '../../components/presentation'
import type { MessageCatalog } from '../../i18n'
import { handoffCreatedJob } from '../../lib/jobHandoff'
import { useWorkspaceMutations } from '../../lib/queries'

export function ImportPage({ messages }: { readonly messages: MessageCatalog }) {
  const [url, setUrl] = useState('')
  const [force, setForce] = useState(false)
  const [error, setError] = useState<string>()
  const mutations = useWorkspaceMutations()
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const trimmedURL = url.trim()
    if (!trimmedURL || mutations.ingestURL.isPending) return
    mutations.ingestURL.mutate({ url: trimmedURL, force }, {
      onSuccess: (job) => {
        setError(undefined)
        handoffCreatedJob(job)
      },
      onError: (reason) => setError(reason instanceof Error ? reason.message : messages.import.failed)
    })
  }
  return (
    <PageStack aria-labelledby="import-title">
      <PageHeader eyebrow={messages.navigation.operations} title={messages.import.title} titleId="import-title" description={messages.import.description} />
      <div className="unavailable-actions">
        <p id="import-note">{messages.import.note}</p>
        <form className="import-form" aria-describedby="import-note" onSubmit={submit}>
          <Toolbar label={messages.import.title} stackAt="medium"
            startContent={
              <ActionGroup align="start" gap="cluster">
                <TextInput label={messages.import.url} value={url} placeholder={messages.import.placeholder} htmlName="article-url" onChange={setUrl} />
                <CheckboxInput label={messages.import.force} value={force} htmlName="force-download" onChange={setForce} />
              </ActionGroup>
            }
            endContent={<Button label={messages.import.submit} type="submit" variant="primary" isLoading={mutations.ingestURL.isPending} isDisabled={!url.trim()} />}
          />
        </form>
        {error ? <p role="alert">{error}</p> : null}
      </div>
    </PageStack>
  )
}
