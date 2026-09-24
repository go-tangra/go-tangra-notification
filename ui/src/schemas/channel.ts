import { z } from 'zod'
import { nonEmpty, optionalString, email, positiveInt } from '@go-tangra/ui/forms'
import { SET_MARKER } from '@/api/types'

export const CHANNEL_TYPES = ['email', 'sms', 'slack', 'sse'] as const
export const TLS_MODES = ['implicit', 'starttls', 'none'] as const

/** Write-only credential: blank keeps the stored value; the stored marker (__set__) round-trips unchanged. */
const secret = optionalString(4096)
export { SET_MARKER }

export const channelSchema = z
  .object({
    name: nonEmpty(200),
    type: z.enum(CHANNEL_TYPES),
    enabled: z.boolean().optional().transform((v) => v ?? false),
    is_default: z.boolean().optional().transform((v) => v ?? false),
    host: optionalString(253),
    port: positiveInt.pipe(z.number().max(65535, 'Ports go up to 65535.')),
    tls: z.enum(TLS_MODES).optional().transform((v) => v ?? 'starttls'),
    username: optionalString(200),
    password: secret,
    from: optionalString(320).pipe(email.optional()),
    reply_to: optionalString(320).pipe(email.optional()),
    account: optionalString(200),
    api_key: secret,
  })
  .superRefine((o, ctx) => {
    if (o.type === 'email') {
      if (!o.host) ctx.addIssue({ code: 'custom', path: ['host'], message: 'The SMTP host is required.' })
      if (!o.port) ctx.addIssue({ code: 'custom', path: ['port'], message: 'The SMTP port is required.' })
      if (!o.from) ctx.addIssue({ code: 'custom', path: ['from'], message: 'The From address is required.' })
    } else if (!o.account) ctx.addIssue({ code: 'custom', path: ['account'], message: 'The account is required.' })
  })
export type ChannelFormOutput = z.output<typeof channelSchema>

export const testMessageSchema = z.object({ recipient: nonEmpty(320) })
