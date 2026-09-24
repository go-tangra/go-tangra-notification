// Package schema embeds the JSON schemas the service validates documents with.
package schema

import _ "embed"

// Backup is the accepted tenant backup document (contracts/backup.schema.json).
//
//go:embed backup.schema.json
var Backup []byte
