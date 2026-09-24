import { z } from 'zod'

export const BACKUP_MODES = ['skip', 'overwrite'] as const
export const backupImportSchema = z.object({
  file: z.instanceof(File, { message: 'Choose a backup file.' }).refine((f) => f.size <= 50 * 1024 * 1024, 'The file is larger than 50 MiB.'),
  mode: z.enum(BACKUP_MODES),
})
export const logFilterSchema = z.object({
  status: z.enum(['pending', 'sent', 'failed']).optional(),
  recipient: z.string().trim().max(320).optional(),
})
