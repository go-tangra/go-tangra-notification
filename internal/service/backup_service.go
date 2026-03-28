package service

import (
	"context"
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/go-tangra/go-tangra-common/backup"
	"github.com/go-tangra/go-tangra-common/grpcx"

	notificationV1 "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/internalmessage"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/internalmessagecategory"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/internalmessagerecipient"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/notificationlog"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/template"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/templatepermission"
)

const (
	backupModule        = "notification"
	backupSchemaVersion = 1
)

// Migrations registry -- add entries here when schema changes.
var backupMigrations = backup.NewMigrationRegistry(backupModule)

// Register migrations in init. Example for future use:
//
//	func init() {
//	    backupMigrations.Register(1, func(entities map[string]json.RawMessage) error {
//	        return backup.MigrateAddField(entities, "channels", "newField", "")
//	    })
//	}

type BackupService struct {
	notificationV1.UnimplementedBackupServiceServer

	log       *log.Helper
	entClient *entCrud.EntClient[*ent.Client]
}

func NewBackupService(ctx *bootstrap.Context, entClient *entCrud.EntClient[*ent.Client]) *BackupService {
	return &BackupService{
		log:       ctx.NewLoggerHelper("notification/service/backup"),
		entClient: entClient,
	}
}

// ExportBackup exports all notification entities as a gzipped archive.
func (s *BackupService) ExportBackup(ctx context.Context, req *notificationV1.ExportBackupRequest) (*notificationV1.ExportBackupResponse, error) {
	tenantID := grpcx.GetTenantIDFromContext(ctx)
	full := false

	if grpcx.IsPlatformAdmin(ctx) && req.TenantId != nil && *req.TenantId == 0 {
		full = true
		tenantID = 0
	} else if req.TenantId != nil && *req.TenantId != 0 && grpcx.IsPlatformAdmin(ctx) {
		tenantID = *req.TenantId
	}

	client := s.entClient.Client()
	a := backup.NewArchive(backupModule, backupSchemaVersion, tenantID, full)

	// Export channels
	if err := s.exportChannels(ctx, client, a, tenantID, full); err != nil {
		return nil, err
	}

	// Export templates (FK: channel_id -> channels)
	if err := s.exportTemplates(ctx, client, a, tenantID, full); err != nil {
		return nil, err
	}

	// Export template permissions (FK: resource_id -> templates/channels)
	if err := s.exportTemplatePermissions(ctx, client, a, tenantID, full); err != nil {
		return nil, err
	}

	// Export notification logs (FK: channel_id, template_id)
	if err := s.exportNotificationLogs(ctx, client, a, tenantID, full); err != nil {
		return nil, err
	}

	// Export internal message categories
	if err := s.exportInternalMessageCategories(ctx, client, a, tenantID, full); err != nil {
		return nil, err
	}

	// Export internal messages (FK: category_id -> internal_message_categories)
	if err := s.exportInternalMessages(ctx, client, a, tenantID, full); err != nil {
		return nil, err
	}

	// Export internal message recipients (FK: message_id -> internal_messages)
	if err := s.exportInternalMessageRecipients(ctx, client, a, tenantID, full); err != nil {
		return nil, err
	}

	// Pack (JSON + gzip)
	data, err := backup.Pack(a)
	if err != nil {
		return nil, fmt.Errorf("pack backup: %w", err)
	}

	s.log.Infof("exported backup: module=%s tenant=%d full=%v entities=%v", backupModule, tenantID, full, a.Manifest.EntityCounts)

	return &notificationV1.ExportBackupResponse{
		Data:          data,
		Module:        backupModule,
		Version:       fmt.Sprintf("%d", backupSchemaVersion),
		ExportedAt:    timestamppb.New(a.Manifest.ExportedAt),
		TenantId:      tenantID,
		EntityCounts:  a.Manifest.EntityCounts,
		SchemaVersion: int32(backupSchemaVersion),
	}, nil
}

// ImportBackup restores notification entities from a gzipped archive.
func (s *BackupService) ImportBackup(ctx context.Context, req *notificationV1.ImportBackupRequest) (*notificationV1.ImportBackupResponse, error) {
	tenantID := grpcx.GetTenantIDFromContext(ctx)
	isPlatformAdmin := grpcx.IsPlatformAdmin(ctx)
	mode := mapRestoreMode(req.GetMode())

	// Unpack
	a, err := backup.Unpack(req.GetData())
	if err != nil {
		return nil, fmt.Errorf("unpack backup: %w", err)
	}

	// Validate
	if err := backup.Validate(a, backupModule, backupSchemaVersion); err != nil {
		return nil, err
	}

	// Full backups require platform admin
	if a.Manifest.FullBackup && !isPlatformAdmin {
		return nil, fmt.Errorf("only platform admins can restore full backups")
	}

	// Run migrations if needed
	sourceVersion := a.Manifest.SchemaVersion
	applied, err := backupMigrations.RunMigrations(a, backupSchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	// Determine restore tenant
	if !isPlatformAdmin || !a.Manifest.FullBackup {
		tenantID = grpcx.GetTenantIDFromContext(ctx)
	} else {
		tenantID = 0
	}

	client := s.entClient.Client()
	result := backup.NewRestoreResult(sourceVersion, backupSchemaVersion, applied)

	// Import in FK dependency order
	s.importChannels(ctx, client, a, tenantID, a.Manifest.FullBackup, mode, result)
	s.importTemplates(ctx, client, a, tenantID, a.Manifest.FullBackup, mode, result)
	s.importTemplatePermissions(ctx, client, a, tenantID, a.Manifest.FullBackup, mode, result)
	s.importNotificationLogs(ctx, client, a, tenantID, a.Manifest.FullBackup, mode, result)
	s.importInternalMessageCategories(ctx, client, a, tenantID, a.Manifest.FullBackup, mode, result)
	s.importInternalMessages(ctx, client, a, tenantID, a.Manifest.FullBackup, mode, result)
	s.importInternalMessageRecipients(ctx, client, a, tenantID, a.Manifest.FullBackup, mode, result)

	s.log.Infof("imported backup: module=%s tenant=%d mode=%v migrations=%d results=%d",
		backupModule, tenantID, mode, applied, len(result.Results))

	// Convert to proto response
	protoResults := make([]*notificationV1.EntityImportResult, len(result.Results))
	for i, r := range result.Results {
		protoResults[i] = &notificationV1.EntityImportResult{
			EntityType: r.EntityType,
			Total:      r.Total,
			Created:    r.Created,
			Updated:    r.Updated,
			Skipped:    r.Skipped,
			Failed:     r.Failed,
		}
	}

	return &notificationV1.ImportBackupResponse{
		Success:           result.Success,
		Results:           protoResults,
		Warnings:          result.Warnings,
		SourceVersion:     int32(result.SourceVersion),
		TargetVersion:     int32(result.TargetVersion),
		MigrationsApplied: int32(result.MigrationsApplied),
	}, nil
}

func mapRestoreMode(m notificationV1.RestoreMode) backup.RestoreMode {
	if m == notificationV1.RestoreMode_RESTORE_MODE_OVERWRITE {
		return backup.RestoreModeOverwrite
	}
	return backup.RestoreModeSkip
}

// --- Export helpers ---

func (s *BackupService) exportChannels(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool) error {
	q := client.Channel.Query()
	if !full {
		q = q.Where(channel.TenantID(tenantID))
	}
	items, err := q.All(ctx)
	if err != nil {
		return fmt.Errorf("export channels: %w", err)
	}
	if err := backup.SetEntities(a, "channels", items); err != nil {
		return fmt.Errorf("set channels: %w", err)
	}
	return nil
}

func (s *BackupService) exportTemplates(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool) error {
	q := client.Template.Query()
	if !full {
		q = q.Where(template.TenantID(tenantID))
	}
	items, err := q.All(ctx)
	if err != nil {
		return fmt.Errorf("export templates: %w", err)
	}
	if err := backup.SetEntities(a, "templates", items); err != nil {
		return fmt.Errorf("set templates: %w", err)
	}
	return nil
}

func (s *BackupService) exportTemplatePermissions(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool) error {
	q := client.TemplatePermission.Query()
	if !full {
		q = q.Where(templatepermission.TenantID(tenantID))
	}
	items, err := q.All(ctx)
	if err != nil {
		return fmt.Errorf("export template permissions: %w", err)
	}
	if err := backup.SetEntities(a, "templatePermissions", items); err != nil {
		return fmt.Errorf("set template permissions: %w", err)
	}
	return nil
}

func (s *BackupService) exportNotificationLogs(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool) error {
	q := client.NotificationLog.Query()
	if !full {
		q = q.Where(notificationlog.TenantID(tenantID))
	}
	items, err := q.All(ctx)
	if err != nil {
		return fmt.Errorf("export notification logs: %w", err)
	}
	if err := backup.SetEntities(a, "notificationLogs", items); err != nil {
		return fmt.Errorf("set notification logs: %w", err)
	}
	return nil
}

func (s *BackupService) exportInternalMessageCategories(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool) error {
	q := client.InternalMessageCategory.Query()
	if !full {
		q = q.Where(internalmessagecategory.TenantID(tenantID))
	}
	items, err := q.All(ctx)
	if err != nil {
		return fmt.Errorf("export internal message categories: %w", err)
	}
	if err := backup.SetEntities(a, "internalMessageCategories", items); err != nil {
		return fmt.Errorf("set internal message categories: %w", err)
	}
	return nil
}

func (s *BackupService) exportInternalMessages(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool) error {
	q := client.InternalMessage.Query()
	if !full {
		q = q.Where(internalmessage.TenantID(tenantID))
	}
	items, err := q.All(ctx)
	if err != nil {
		return fmt.Errorf("export internal messages: %w", err)
	}
	if err := backup.SetEntities(a, "internalMessages", items); err != nil {
		return fmt.Errorf("set internal messages: %w", err)
	}
	return nil
}

func (s *BackupService) exportInternalMessageRecipients(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool) error {
	q := client.InternalMessageRecipient.Query()
	if !full {
		q = q.Where(internalmessagerecipient.TenantID(tenantID))
	}
	items, err := q.All(ctx)
	if err != nil {
		return fmt.Errorf("export internal message recipients: %w", err)
	}
	if err := backup.SetEntities(a, "internalMessageRecipients", items); err != nil {
		return fmt.Errorf("set internal message recipients: %w", err)
	}
	return nil
}

// --- Import helpers ---

func (s *BackupService) importChannels(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool, mode backup.RestoreMode, result *backup.RestoreResult) {
	items, err := backup.GetEntities[ent.Channel](a, "channels")
	if err != nil {
		result.AddWarning(fmt.Sprintf("channels: unmarshal error: %v", err))
		return
	}
	if len(items) == 0 {
		return
	}

	er := backup.EntityResult{EntityType: "channels", Total: int64(len(items))}

	for _, e := range items {
		tid := tenantID
		if full && e.TenantID != nil {
			tid = *e.TenantID
		}

		existing, getErr := client.Channel.Get(ctx, e.ID)
		if getErr != nil && !ent.IsNotFound(getErr) {
			result.AddWarning(fmt.Sprintf("channels: lookup %s: %v", e.ID, getErr))
			er.Failed++
			continue
		}
		if existing != nil {
			if mode == backup.RestoreModeSkip {
				er.Skipped++
				continue
			}
			_, err := client.Channel.UpdateOneID(e.ID).
				SetName(e.Name).
				SetType(e.Type).
				SetConfig(e.Config).
				SetEnabled(e.Enabled).
				SetIsDefault(e.IsDefault).
				SetNillableCreateBy(e.CreateBy).
				SetNillableUpdateBy(e.UpdateBy).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("channels: update %s: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Updated++
		} else {
			_, err := client.Channel.Create().
				SetID(e.ID).
				SetNillableTenantID(&tid).
				SetName(e.Name).
				SetType(e.Type).
				SetConfig(e.Config).
				SetEnabled(e.Enabled).
				SetIsDefault(e.IsDefault).
				SetNillableCreateBy(e.CreateBy).
				SetNillableUpdateBy(e.UpdateBy).
				SetNillableCreateTime(e.CreateTime).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("channels: create %s: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Created++
		}
	}

	result.AddResult(er)
}

func (s *BackupService) importTemplates(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool, mode backup.RestoreMode, result *backup.RestoreResult) {
	items, err := backup.GetEntities[ent.Template](a, "templates")
	if err != nil {
		result.AddWarning(fmt.Sprintf("templates: unmarshal error: %v", err))
		return
	}
	if len(items) == 0 {
		return
	}

	er := backup.EntityResult{EntityType: "templates", Total: int64(len(items))}

	for _, e := range items {
		tid := tenantID
		if full && e.TenantID != nil {
			tid = *e.TenantID
		}

		existing, getErr := client.Template.Get(ctx, e.ID)
		if getErr != nil && !ent.IsNotFound(getErr) {
			result.AddWarning(fmt.Sprintf("templates: lookup %s: %v", e.ID, getErr))
			er.Failed++
			continue
		}
		if existing != nil {
			if mode == backup.RestoreModeSkip {
				er.Skipped++
				continue
			}
			_, err := client.Template.UpdateOneID(e.ID).
				SetName(e.Name).
				SetChannelID(e.ChannelID).
				SetSubject(e.Subject).
				SetBody(e.Body).
				SetVariables(e.Variables).
				SetIsDefault(e.IsDefault).
				SetNillableCreateBy(e.CreateBy).
				SetNillableUpdateBy(e.UpdateBy).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("templates: update %s: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Updated++
		} else {
			_, err := client.Template.Create().
				SetID(e.ID).
				SetNillableTenantID(&tid).
				SetName(e.Name).
				SetChannelID(e.ChannelID).
				SetSubject(e.Subject).
				SetBody(e.Body).
				SetVariables(e.Variables).
				SetIsDefault(e.IsDefault).
				SetNillableCreateBy(e.CreateBy).
				SetNillableUpdateBy(e.UpdateBy).
				SetNillableCreateTime(e.CreateTime).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("templates: create %s: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Created++
		}
	}

	result.AddResult(er)
}

func (s *BackupService) importTemplatePermissions(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool, mode backup.RestoreMode, result *backup.RestoreResult) {
	items, err := backup.GetEntities[ent.TemplatePermission](a, "templatePermissions")
	if err != nil {
		result.AddWarning(fmt.Sprintf("templatePermissions: unmarshal error: %v", err))
		return
	}
	if len(items) == 0 {
		return
	}

	er := backup.EntityResult{EntityType: "templatePermissions", Total: int64(len(items))}

	for _, e := range items {
		tid := tenantID
		if full && e.TenantID != nil {
			tid = *e.TenantID
		}

		existing, getErr := client.TemplatePermission.Get(ctx, e.ID)
		if getErr != nil && !ent.IsNotFound(getErr) {
			result.AddWarning(fmt.Sprintf("templatePermissions: lookup %d: %v", e.ID, getErr))
			er.Failed++
			continue
		}
		if existing != nil {
			if mode == backup.RestoreModeSkip {
				er.Skipped++
				continue
			}
			_, err := client.TemplatePermission.UpdateOneID(e.ID).
				SetResourceType(e.ResourceType).
				SetResourceID(e.ResourceID).
				SetRelation(e.Relation).
				SetSubjectType(e.SubjectType).
				SetSubjectID(e.SubjectID).
				SetNillableGrantedBy(e.GrantedBy).
				SetNillableExpiresAt(e.ExpiresAt).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("templatePermissions: update %d: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Updated++
		} else {
			_, err := client.TemplatePermission.Create().
				SetNillableTenantID(&tid).
				SetResourceType(e.ResourceType).
				SetResourceID(e.ResourceID).
				SetRelation(e.Relation).
				SetSubjectType(e.SubjectType).
				SetSubjectID(e.SubjectID).
				SetNillableGrantedBy(e.GrantedBy).
				SetNillableExpiresAt(e.ExpiresAt).
				SetNillableCreateTime(e.CreateTime).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("templatePermissions: create: %v", err))
				er.Failed++
				continue
			}
			er.Created++
		}
	}

	result.AddResult(er)
}

func (s *BackupService) importNotificationLogs(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool, mode backup.RestoreMode, result *backup.RestoreResult) {
	items, err := backup.GetEntities[ent.NotificationLog](a, "notificationLogs")
	if err != nil {
		result.AddWarning(fmt.Sprintf("notificationLogs: unmarshal error: %v", err))
		return
	}
	if len(items) == 0 {
		return
	}

	er := backup.EntityResult{EntityType: "notificationLogs", Total: int64(len(items))}

	for _, e := range items {
		tid := tenantID
		if full && e.TenantID != nil {
			tid = *e.TenantID
		}

		existing, getErr := client.NotificationLog.Get(ctx, e.ID)
		if getErr != nil && !ent.IsNotFound(getErr) {
			result.AddWarning(fmt.Sprintf("notificationLogs: lookup %s: %v", e.ID, getErr))
			er.Failed++
			continue
		}
		if existing != nil {
			if mode == backup.RestoreModeSkip {
				er.Skipped++
				continue
			}
			_, err := client.NotificationLog.UpdateOneID(e.ID).
				SetChannelID(e.ChannelID).
				SetChannelType(e.ChannelType).
				SetTemplateID(e.TemplateID).
				SetRecipient(e.Recipient).
				SetRenderedSubject(e.RenderedSubject).
				SetRenderedBody(e.RenderedBody).
				SetStatus(e.Status).
				SetErrorMessage(e.ErrorMessage).
				SetNillableSentAt(e.SentAt).
				SetNillableCreateBy(e.CreateBy).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("notificationLogs: update %s: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Updated++
		} else {
			_, err := client.NotificationLog.Create().
				SetID(e.ID).
				SetNillableTenantID(&tid).
				SetChannelID(e.ChannelID).
				SetChannelType(e.ChannelType).
				SetTemplateID(e.TemplateID).
				SetRecipient(e.Recipient).
				SetRenderedSubject(e.RenderedSubject).
				SetRenderedBody(e.RenderedBody).
				SetStatus(e.Status).
				SetErrorMessage(e.ErrorMessage).
				SetNillableSentAt(e.SentAt).
				SetNillableCreateBy(e.CreateBy).
				SetNillableCreateTime(e.CreateTime).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("notificationLogs: create %s: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Created++
		}
	}

	result.AddResult(er)
}

func (s *BackupService) importInternalMessageCategories(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool, mode backup.RestoreMode, result *backup.RestoreResult) {
	items, err := backup.GetEntities[ent.InternalMessageCategory](a, "internalMessageCategories")
	if err != nil {
		result.AddWarning(fmt.Sprintf("internalMessageCategories: unmarshal error: %v", err))
		return
	}
	if len(items) == 0 {
		return
	}

	er := backup.EntityResult{EntityType: "internalMessageCategories", Total: int64(len(items))}

	for _, e := range items {
		tid := tenantID
		if full && e.TenantID != nil {
			tid = *e.TenantID
		}

		existing, getErr := client.InternalMessageCategory.Get(ctx, e.ID)
		if getErr != nil && !ent.IsNotFound(getErr) {
			result.AddWarning(fmt.Sprintf("internalMessageCategories: lookup %d: %v", e.ID, getErr))
			er.Failed++
			continue
		}
		if existing != nil {
			if mode == backup.RestoreModeSkip {
				er.Skipped++
				continue
			}
			_, err := client.InternalMessageCategory.UpdateOneID(e.ID).
				SetNillableName(e.Name).
				SetNillableCode(e.Code).
				SetNillableIconURL(e.IconURL).
				SetNillableIsEnabled(e.IsEnabled).
				SetNillableSortOrder(e.SortOrder).
				SetNillableRemark(e.Remark).
				SetNillableCreatedBy(e.CreatedBy).
				SetNillableUpdatedBy(e.UpdatedBy).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("internalMessageCategories: update %d: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Updated++
		} else {
			_, err := client.InternalMessageCategory.Create().
				SetID(e.ID).
				SetNillableTenantID(&tid).
				SetNillableName(e.Name).
				SetNillableCode(e.Code).
				SetNillableIconURL(e.IconURL).
				SetNillableIsEnabled(e.IsEnabled).
				SetNillableSortOrder(e.SortOrder).
				SetNillableRemark(e.Remark).
				SetNillableCreatedBy(e.CreatedBy).
				SetNillableUpdatedBy(e.UpdatedBy).
				SetNillableCreatedAt(e.CreatedAt).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("internalMessageCategories: create %d: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Created++
		}
	}

	result.AddResult(er)
}

func (s *BackupService) importInternalMessages(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool, mode backup.RestoreMode, result *backup.RestoreResult) {
	items, err := backup.GetEntities[ent.InternalMessage](a, "internalMessages")
	if err != nil {
		result.AddWarning(fmt.Sprintf("internalMessages: unmarshal error: %v", err))
		return
	}
	if len(items) == 0 {
		return
	}

	er := backup.EntityResult{EntityType: "internalMessages", Total: int64(len(items))}

	for _, e := range items {
		tid := tenantID
		if full && e.TenantID != nil {
			tid = *e.TenantID
		}

		existing, getErr := client.InternalMessage.Get(ctx, e.ID)
		if getErr != nil && !ent.IsNotFound(getErr) {
			result.AddWarning(fmt.Sprintf("internalMessages: lookup %d: %v", e.ID, getErr))
			er.Failed++
			continue
		}
		if existing != nil {
			if mode == backup.RestoreModeSkip {
				er.Skipped++
				continue
			}
			_, err := client.InternalMessage.UpdateOneID(e.ID).
				SetNillableTitle(e.Title).
				SetNillableContent(e.Content).
				SetNillableSenderID(e.SenderID).
				SetNillableCategoryID(e.CategoryID).
				SetNillableStatus(e.Status).
				SetNillableType(e.Type).
				SetNillableCreatedBy(e.CreatedBy).
				SetNillableUpdatedBy(e.UpdatedBy).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("internalMessages: update %d: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Updated++
		} else {
			cr := client.InternalMessage.Create().
				SetID(e.ID).
				SetNillableTenantID(&tid).
				SetNillableTitle(e.Title).
				SetNillableContent(e.Content).
				SetNillableCategoryID(e.CategoryID).
				SetNillableStatus(e.Status).
				SetNillableType(e.Type).
				SetNillableCreatedBy(e.CreatedBy).
				SetNillableUpdatedBy(e.UpdatedBy).
				SetNillableCreatedAt(e.CreatedAt)
			if e.SenderID != nil {
				cr = cr.SetSenderID(*e.SenderID)
			}
			_, err := cr.Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("internalMessages: create %d: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Created++
		}
	}

	result.AddResult(er)
}

func (s *BackupService) importInternalMessageRecipients(ctx context.Context, client *ent.Client, a *backup.Archive, tenantID uint32, full bool, mode backup.RestoreMode, result *backup.RestoreResult) {
	items, err := backup.GetEntities[ent.InternalMessageRecipient](a, "internalMessageRecipients")
	if err != nil {
		result.AddWarning(fmt.Sprintf("internalMessageRecipients: unmarshal error: %v", err))
		return
	}
	if len(items) == 0 {
		return
	}

	er := backup.EntityResult{EntityType: "internalMessageRecipients", Total: int64(len(items))}

	for _, e := range items {
		tid := tenantID
		if full && e.TenantID != nil {
			tid = *e.TenantID
		}

		existing, getErr := client.InternalMessageRecipient.Get(ctx, e.ID)
		if getErr != nil && !ent.IsNotFound(getErr) {
			result.AddWarning(fmt.Sprintf("internalMessageRecipients: lookup %d: %v", e.ID, getErr))
			er.Failed++
			continue
		}
		if existing != nil {
			if mode == backup.RestoreModeSkip {
				er.Skipped++
				continue
			}
			_, err := client.InternalMessageRecipient.UpdateOneID(e.ID).
				SetNillableMessageID(e.MessageID).
				SetNillableRecipientUserID(e.RecipientUserID).
				SetNillableStatus(e.Status).
				SetNillableReceivedAt(e.ReceivedAt).
				SetNillableReadAt(e.ReadAt).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("internalMessageRecipients: update %d: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Updated++
		} else {
			_, err := client.InternalMessageRecipient.Create().
				SetID(e.ID).
				SetNillableTenantID(&tid).
				SetNillableMessageID(e.MessageID).
				SetNillableRecipientUserID(e.RecipientUserID).
				SetNillableStatus(e.Status).
				SetNillableReceivedAt(e.ReceivedAt).
				SetNillableReadAt(e.ReadAt).
				SetNillableCreatedAt(e.CreatedAt).
				Save(ctx)
			if err != nil {
				result.AddWarning(fmt.Sprintf("internalMessageRecipients: create %d: %v", e.ID, err))
				er.Failed++
				continue
			}
			er.Created++
		}
	}

	result.AddResult(er)
}
