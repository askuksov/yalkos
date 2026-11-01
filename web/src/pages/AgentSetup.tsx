import { Trans, useTranslation } from 'react-i18next'

import { Button } from '../ui/Button'

function Snippet({ label, text }: { label?: string; text: string }) {
  const { t } = useTranslation('common')
  return (
    <div>
      {label && <h3>{label}</h3>}
      <pre className="snippet">
        <code>{text}</code>
      </pre>
      <Button size="sm" onClick={() => void navigator.clipboard?.writeText(text)}>
        {t('actions.copy')}
      </Button>
    </div>
  )
}

/** Commands that connect Claude Code and OpenCode to this server's MCP endpoint. */
export function AgentSetup({ apiKey }: { apiKey?: string }) {
  const { t } = useTranslation('settings')
  const url = window.location.origin + '/mcp'
  const key = apiKey || 'YOUR_API_KEY'
  const claudeCode = `claude mcp add --transport http yalkos ${url} --header "Authorization: Bearer ${key}"`
  // The OpenCode config often lives in a repository: it names the variable,
  // and the key goes into the environment.
  const openCode = JSON.stringify(
    { mcp: { yalkos: { type: 'remote', url, headers: { Authorization: 'Bearer {env:YALKOS_API_KEY}' }, oauth: false } } },
    null,
    2,
  )
  const openCodeKey = `export YALKOS_API_KEY=${key}`
  return (
    <section>
      <h2>{t('apiKeys.agent.title')}</h2>
      <p>
        <Trans t={t} i18nKey="apiKeys.agent.intro" values={{ url }} components={{ code: <code /> }} />
      </p>
      <Snippet label={t('apiKeys.agent.claudeCode')} text={claudeCode} />
      <Snippet label={t('apiKeys.agent.openCode')} text={openCode} />
      <p>{t('apiKeys.agent.openCodeKey')}</p>
      <Snippet text={openCodeKey} />
      <p>{t('apiKeys.agent.warning')}</p>
    </section>
  )
}
