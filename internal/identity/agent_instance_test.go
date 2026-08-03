package identity

import "testing"

// Agent workloads authenticate as agent_instance. Refusing the type here
// refused every turn they tried to take: the LLM proxy answered 401 and codex
// reported "identity type unsupported: IDENTITY_TYPE_AGENT_INSTANCE".
func TestParseIdentityTypeAcceptsAgentInstance(t *testing.T) {
	parsed, err := ParseIdentityType("agent_instance")
	if err != nil {
		t.Fatalf("parse agent_instance: %v", err)
	}
	if parsed != IdentityTypeAgentInstance {
		t.Fatalf("expected %q, got %q", IdentityTypeAgentInstance, parsed)
	}
}

// An instance is not a sandbox, so it must not be mistaken for one.
func TestAgentInstanceIsNotASandbox(t *testing.T) {
	resolved := ResolvedIdentity{IdentityID: "id", IdentityType: IdentityTypeAgentInstance}
	if resolved.SandboxID() != "" {
		t.Fatalf("expected no sandbox id, got %q", resolved.SandboxID())
	}
}
