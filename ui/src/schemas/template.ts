import { z } from 'zod'
import { nonEmpty, optionalString } from '@go-tangra/ui/forms'

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

/** System templates: only the wording changes (name, channel and variables are fixed). */
export const systemTemplateSchema = z.object({
  subject: nonEmpty(500),
  body: nonEmpty(65536),
})
export type SystemTemplateFormOutput = z.output<typeof systemTemplateSchema>

export const templateSearchSchema = z.object({ q: optionalString(200) })
