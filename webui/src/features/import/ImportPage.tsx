import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { TextInput } from '@astryxdesign/core/TextInput'
import { useState } from 'react'
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
    <section aria-labelledby="import-title">
      <header className="page-heading">
        <div>
          <p className="eyebrow">{messages.navigation.operations}</p>
          <h1 id="import-title">{messages.import.title}</h1>
          <p className="lede">{messages.import.description}</p>
        </div>
      </header>
      <section className="unavailable-actions" aria-labelledby="import-form-title">
        <div><h2 id="import-form-title">{messages.import.title}</h2><p>{messages.import.note}</p></div>
        <form className="import-form" onSubmit={submit}>
          <TextInput label={messages.import.url} value={url} placeholder={messages.import.placeholder} onChange={setUrl} />
          <CheckboxInput label={messages.import.force} value={force} onChange={setForce} />
          <Button label={messages.import.submit} type="submit" variant="primary" isLoading={mutations.ingestURL.isPending} isDisabled={!url.trim()} />
        </form>
        {error ? <p role="status">{error}</p> : null}
      </section>
    </section>
  )
}
