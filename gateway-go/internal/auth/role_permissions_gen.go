// Hand-written constants. Previously generated from YAML.

package auth

// rolePermissions defines the default scopes for each role.
var rolePermissions = map[Role][]Scope{
	RoleOperator: {ScopeAdmin, ScopeRead, ScopeWrite, ScopeApprovals, ScopePairing},
	RoleViewer:   {ScopeRead},
	RoleAgent:    {ScopeRead, ScopeWrite},
	RoleProbe:    {ScopeRead},
}
