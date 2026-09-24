import { z } from 'zod'
import { nonEmpty, optionalString, isoDate } from '@go-tangra/ui/forms'

export const MESSAGE_TYPES = ['notification', 'private', 'group'] as const
export const MESSAGE_STATUSES = ['draft', 'scheduled', 'published', 'revoked', 'archived'] as const

export const messageSchema = z
  .object({
    title: nonEmpty(300),
    content: nonEmpty(20000),
    type: z.enum(MESSAGE_TYPES),
    category_id: optionalString(64),
    everyone: z.boolean().optional().transform((v) => v ?? false),
    users: z.array(z.string()).optional().transform((v) => v ?? []),
    scheduled_at: isoDate,
  })
  .refine((m) => m.everyone || m.users.length > 0, { path: ['users'], message: 'Pick at least one recipient or address everyone.' })
  .refine((m) => !m.scheduled_at || Date.parse(m.scheduled_at) > Date.now() - 60_000, { path: ['scheduled_at'], message: 'Schedule a time in the future.' })
export type MessageFormOutput = z.output<typeof messageSchema>

export const messageFilterSchema = z.object({ status: z.enum(MESSAGE_STATUSES).optional() })
