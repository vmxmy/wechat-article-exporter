import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { TextInput } from '@astryxdesign/core/TextInput'
import { useState } from 'react'
import { PageHeader, PageStack } from '../../components/presentation'
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
      <section className="unavailable-actions" aria-labelledby="import-form-title">
        <div><h2 id="import-form-title">{messages.import.title}</h2><p>{messages.import.note}</p></div>
        <form className="import-form" onSubmit={submit}>
          <TextInput label={messages.import.url} value={url} placeholder={messages.import.placeholder} htmlName="article-url" onChange={setUrl} />
          <CheckboxInput label={messages.import.force} value={force} htmlName="force-download" onChange={setForce} />
          <Button label={messages.import.submit} type="submit" variant="primary" isLoading={mutations.ingestURL.isPending} isDisabled={!url.trim()} />
        </form>
        {error ? <p role="status">{error}</p> : null}
      </section>
    </PageStack>
  )
}
