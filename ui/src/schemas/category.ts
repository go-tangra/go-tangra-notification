import { z } from 'zod'
import { nonEmpty, optionalString, positiveInt } from '@go-tangra/ui/forms'

export const categorySchema = z.object({
  name: nonEmpty(120),
  description: optionalString(1000),
  sort: positiveInt,
})
export type CategoryFormOutput = z.output<typeof categorySchema>
