import { Button } from '@astryxdesign/core/Button'
import { TextInput } from '@astryxdesign/core/TextInput'
import { useState } from 'react'
import type { MessageCatalog } from '../../i18n'
import { useWorkspaceMutations } from '../../lib/queries'

export function ImportPage({ messages }: { readonly messages: MessageCatalog }) {
  const [url, setUrl] = useState('')
  const [force, setForce] = useState(false)
  const [result, setResult] = useState<string>()
  const mutations = useWorkspaceMutations()
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const trimmedURL = url.trim()
    if (!trimmedURL || mutations.ingestURL.isPending) return
    mutations.ingestURL.mutate({ url: trimmedURL, force }, { onSuccess: (job) => setResult(messages.import.queued(job.id)), onError: (reason) => setResult(reason instanceof Error ? reason.message : messages.import.failed) })
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
          <label className="force-control"><input type="checkbox" checked={force} onChange={(event) => setForce(event.target.checked)} /> {messages.import.force}</label>
          <Button label={messages.import.submit} type="submit" variant="primary" isLoading={mutations.ingestURL.isPending} isDisabled={!url.trim()} />
        </form>
        {result ? <p role="status">{result}</p> : null}
      </section>
    </section>
  )
}
