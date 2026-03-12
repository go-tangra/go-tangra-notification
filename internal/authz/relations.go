package authz

// Relation represents a permission level in the Zanzibar-like authorization system
type Relation string

const (
	// RelationOwner grants full control: read, write, share, delete, use
	RelationOwner Relation = "RELATION_OWNER"
	// RelationEditor grants modify access: read, write, use
	RelationEditor Relation = "RELATION_EDITOR"
	// RelationViewer grants read-only access: read, use
	RelationViewer Relation = "RELATION_VIEWER"
	// RelationSharer grants share access: read, share, use
	RelationSharer Relation = "RELATION_SHARER"
)

// Permission represents an action that can be performed on a resource
type Permission string

const (
	// PermissionRead allows viewing the resource
	PermissionRead Permission = "PERMISSION_READ"
	// PermissionWrite allows modifying the resource
	PermissionWrite Permission = "PERMISSION_WRITE"
	// PermissionDelete allows deleting the resource
	PermissionDelete Permission = "PERMISSION_DELETE"
	// PermissionShare allows sharing the resource with others
	PermissionShare Permission = "PERMISSION_SHARE"
	// PermissionUse allows rendering/sending with the template (relevant for clients)
	PermissionUse Permission = "PERMISSION_USE"
)

// ResourceType represents the type of resource being protected
type ResourceType string

const (
	// ResourceTypeTemplate represents a notification template resource
	ResourceTypeTemplate ResourceType = "RESOURCE_TYPE_TEMPLATE"
	// ResourceTypeChannel represents a notification channel resource
	ResourceTypeChannel ResourceType = "RESOURCE_TYPE_CHANNEL"
)

// WildcardSubjectID is a special subject_id that grants access to all clients
const WildcardSubjectID = "*"

// SubjectType represents the type of entity being granted access
type SubjectType string

const (
	// SubjectTypeUser represents a user subject
	SubjectTypeUser SubjectType = "SUBJECT_TYPE_USER"
	// SubjectTypeRole represents a role subject
	SubjectTypeRole SubjectType = "SUBJECT_TYPE_ROLE"
	// SubjectTypeClient represents an mTLS client subject (e.g. "sharing", "deployer")
	SubjectTypeClient SubjectType = "SUBJECT_TYPE_CLIENT"
)

// AllPermissions is the complete list of permissions for iteration
var AllPermissions = []Permission{PermissionRead, PermissionWrite, PermissionDelete, PermissionShare, PermissionUse}

// relationPermissions defines which permissions each relation grants
// Owner gets all, Editor gets read+write+use, Viewer gets read+use, Sharer gets read+share+use
var relationPermissions = map[Relation][]Permission{
	RelationOwner:  {PermissionRead, PermissionWrite, PermissionDelete, PermissionShare, PermissionUse},
	RelationEditor: {PermissionRead, PermissionWrite, PermissionUse},
	RelationViewer: {PermissionRead, PermissionUse},
	RelationSharer: {PermissionRead, PermissionShare, PermissionUse},
}

// RelationGrantsPermission checks if a relation grants a specific permission
func RelationGrantsPermission(relation Relation, permission Permission) bool {
	permissions, ok := relationPermissions[relation]
	if !ok {
		return false
	}
	for _, p := range permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// GetPermissionsForRelation returns all permissions granted by a relation
func GetPermissionsForRelation(relation Relation) []Permission {
	permissions, ok := relationPermissions[relation]
	if !ok {
		return nil
	}
	result := make([]Permission, len(permissions))
	copy(result, permissions)
	return result
}

// CompareRelations compares two relations by hierarchy level and returns:
// -1 if r1 is lower than r2
//
//	0 if they are the same level
//	1 if r1 is higher than r2
func CompareRelations(r1, r2 Relation) int {
	h1 := RelationHierarchy[r1]
	h2 := RelationHierarchy[r2]
	if h1 < h2 {
		return -1
	}
	if h1 > h2 {
		return 1
	}
	return 0
}

// RelationPermissionsAreSuperset returns true if the permissions granted by
// granterRelation are a superset of the permissions granted by targetRelation.
// This is used for escalation prevention: a granter can only grant a relation
// whose permissions are all already covered by the granter's own relation.
func RelationPermissionsAreSuperset(granterRelation, targetRelation Relation) bool {
	granterPerms := relationPermissions[granterRelation]
	targetPerms := relationPermissions[targetRelation]
	granterSet := make(map[Permission]bool, len(granterPerms))
	for _, p := range granterPerms {
		granterSet[p] = true
	}
	for _, p := range targetPerms {
		if !granterSet[p] {
			return false
		}
	}
	return true
}

// GetHighestRelation returns the relation with the most permissions from a list
func GetHighestRelation(relations []Relation) Relation {
	if len(relations) == 0 {
		return ""
	}
	highest := relations[0]
	for _, r := range relations[1:] {
		if CompareRelations(r, highest) > 0 {
			highest = r
		}
	}
	return highest
}

// RelationHierarchy defines inheritance order (higher = more permissions)
var RelationHierarchy = map[Relation]int{
	RelationOwner:  4,
	RelationEditor: 3,
	RelationSharer: 2,
	RelationViewer: 1,
}

// IsRelationAtLeast checks if r1 has at least as many permissions as r2
func IsRelationAtLeast(r1, r2 Relation) bool {
	return RelationHierarchy[r1] >= RelationHierarchy[r2]
}
