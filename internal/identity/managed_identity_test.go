package identity

import "testing"

func TestParseManagedIdentityName(t *testing.T) {
	resolved, ok := ParseManagedIdentityName("agent-9e2b5e44-965a-4a6d-a1ea-2cf9b8b49ab2-acde1234")
	if !ok {
		t.Fatalf("expected managed identity")
	}
	if resolved.IdentityID != "9e2b5e44-965a-4a6d-a1ea-2cf9b8b49ab2" {
		t.Fatalf("unexpected identity id %q", resolved.IdentityID)
	}
	if resolved.IdentityType != IdentityTypeAgent {
		t.Fatalf("unexpected identity type %q", resolved.IdentityType)
	}
}

func TestParseManagedIdentityNameInvalid(t *testing.T) {
	if _, ok := ParseManagedIdentityName(""); ok {
		t.Fatalf("expected empty identity to be rejected")
	}
	if _, ok := ParseManagedIdentityName("svc-runner-acde1234"); ok {
		t.Fatalf("expected non-agent identity to be rejected")
	}
	if _, ok := ParseManagedIdentityName("agent-not-a-uuid-acde1234"); ok {
		t.Fatalf("expected invalid uuid to be rejected")
	}
}
