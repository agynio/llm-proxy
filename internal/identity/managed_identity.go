package identity

import (
	"strings"

	"github.com/google/uuid"
)

const (
	managedAgentPrefix   = "agent-"
	managedAgentUUIDSize = 36
)

// ParseManagedIdentityName parses managed identity names formatted as
// "agent-<uuid>-<suffix>". The suffix must be non-empty and may contain
// hyphens; it is ignored once the UUID is validated.
func ParseManagedIdentityName(name string) (ResolvedIdentity, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ResolvedIdentity{}, false
	}
	if !strings.HasPrefix(trimmed, managedAgentPrefix) {
		return ResolvedIdentity{}, false
	}

	remainder := strings.TrimPrefix(trimmed, managedAgentPrefix)
	if len(remainder) < managedAgentUUIDSize+2 {
		return ResolvedIdentity{}, false
	}
	if remainder[managedAgentUUIDSize] != '-' {
		return ResolvedIdentity{}, false
	}

	agentID := remainder[:managedAgentUUIDSize]
	if _, err := uuid.Parse(agentID); err != nil {
		return ResolvedIdentity{}, false
	}

	return ResolvedIdentity{
		IdentityID:   agentID,
		IdentityType: IdentityTypeAgent,
	}, true
}
