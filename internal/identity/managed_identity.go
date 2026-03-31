package identity

import (
	"strings"

	"github.com/google/uuid"
)

const managedAgentPrefix = "agent-"

func ParseManagedIdentityName(name string) (ResolvedIdentity, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ResolvedIdentity{}, false
	}
	if !strings.HasPrefix(trimmed, managedAgentPrefix) {
		return ResolvedIdentity{}, false
	}

	remainder := strings.TrimPrefix(trimmed, managedAgentPrefix)
	separator := strings.LastIndex(remainder, "-")
	if separator <= 0 {
		return ResolvedIdentity{}, false
	}

	agentID := remainder[:separator]
	if _, err := uuid.Parse(agentID); err != nil {
		return ResolvedIdentity{}, false
	}

	return ResolvedIdentity{
		IdentityID:   agentID,
		IdentityType: IdentityTypeAgent,
	}, true
}
