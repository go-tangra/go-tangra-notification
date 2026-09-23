import { z } from 'zod'
import { nonEmpty, optionalString } from '@freya/ui/forms'

const variableName = z.string().regex(/^[A-Za-z_][A-Za-z0-9_]*$/, 'Variables are identifiers (letters, digits, _).')

export const templateSchema = z.object({
  name: nonEmpty(200),
  channel_id: nonEmpty(64),
  subject: nonEmpty(500),
  body: nonEmpty(65536),
  variables: z.array(variableName).max(50).optional().transform((v) => v ?? []),
  is_default: z.boolean().optional().transform((v) => v ?? false),
})
export type TemplateFormOutput = z.output<typeof templateSchema>

export const templateSearchSchema = z.object({ q: optionalString(200) })
