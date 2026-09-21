// Package openapi embeds the notification browser API contract.
package openapi

import _ "embed"

// Notification is the OpenAPI 3.1 document served and validated by the service.
//
//go:embed notification.yaml
var Notification []byte
