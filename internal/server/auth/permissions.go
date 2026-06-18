package auth

import "github.com/lingyuins/octopus/internal/model"

// Permission represents a fine-grained access right.
type Permission string

const (
	PermChannelsRead  Permission = "channels:read"
	PermChannelsWrite Permission = "channels:write"
	PermGroupsRead    Permission = "groups:read"
	PermGroupsWrite   Permission = "groups:write"
	PermAPIKeysRead   Permission = "apikeys:read"
	PermAPIKeysWrite  Permission = "apikeys:write"
	PermSettingsRead  Permission = "settings:read"
	PermSettingsWrite Permission = "settings:write"
	PermLogsRead      Permission = "logs:read"
	PermLogsWrite     Permission = "logs:write"
	PermStatsRead     Permission = "stats:read"
	PermUsersRead     Permission = "users:read"
	PermUsersWrite    Permission = "users:write"
	PermSitesRead     Permission = "sites:read"
	PermSitesWrite    Permission = "sites:write"
)

var adminPermissions = []Permission{
	PermChannelsRead, PermChannelsWrite,
	PermGroupsRead, PermGroupsWrite,
	PermAPIKeysRead, PermAPIKeysWrite,
	PermSettingsRead, PermSettingsWrite,
	PermLogsRead, PermLogsWrite, PermStatsRead,
	PermUsersRead, PermUsersWrite,
	PermSitesRead, PermSitesWrite,
}

var editorPermissions = []Permission{
	PermChannelsRead, PermChannelsWrite,
	PermGroupsRead, PermGroupsWrite,
	PermAPIKeysRead, PermAPIKeysWrite,
	PermSettingsRead, PermSettingsWrite,
	PermLogsRead, PermLogsWrite, PermStatsRead,
	PermSitesRead, PermSitesWrite,
}

var viewerPermissions = []Permission{
	PermChannelsRead,
	PermGroupsRead,
	PermAPIKeysRead,
	PermSettingsRead,
	PermLogsRead, PermStatsRead,
	PermSitesRead,
}

// GGZERO commercial: end-customer role. Minimal privileges — manage own API keys
// (ownership-isolated in handlers) + read aggregate stats. Deliberately NO
// settings:read (the settings list exposes secrets like epay_key), and NO
// channels/groups/logs/sites/users — a public registrant sees only their own
// keys + the public site overview, never upstream config or other tenants' data.
var userPermissions = []Permission{
	PermAPIKeysRead, PermAPIKeysWrite,
	PermStatsRead,
}

var rolePermissions = map[string][]Permission{
	model.UserRoleAdmin:  adminPermissions,
	model.UserRoleEditor: editorPermissions,
	model.UserRoleViewer: viewerPermissions,
	model.UserRoleUser:   userPermissions,
}

// HasPermission checks if the given role has the specified permission.
func HasPermission(role string, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}
