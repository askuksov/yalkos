import { useTranslation } from 'react-i18next'

import { isInsecureRemote } from '../lib/transport'

/** Warns when the page is served over plain HTTP from a non-loopback host. Renders nothing otherwise. */
export function InsecureTransportNotice() {
  const { t } = useTranslation('settings')
  if (!isInsecureRemote(window.location)) return null
  // A note, not an alert: it is static context and must not interrupt screen readers on every visit.
  return (
    <div role="note" className="notice">
      <strong>{t('apiKeys.insecure.title')}</strong> {t('apiKeys.insecure.body')}
    </div>
  )
}
