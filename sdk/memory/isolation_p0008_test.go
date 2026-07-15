package memory

import (
	"context"
	"testing"
)

// TestLongTerm_TenantIsolation verifies P0-008: long-term memory is keyed by
// (agentID, userID). Two users served by the same agent must never observe
// each other's long-term memories, and the empty-userID (global/tenantless)
// bucket must keep working without leaking into named tenants.
func TestLongTerm_TenantIsolation(t *testing.T) {
	backend := newMemStorage()
	ctx := context.Background()

	alice := NewStoreForUser("agent1", "alice", backend)
	bob := NewStoreForUser("agent1", "bob", backend)
	global := NewStore("agent1", backend) // empty userID -> global bucket

	// Same logical key, different tenants and values.
	if err := alice.SetLongTerm(ctx, "secret", "alice-secret"); err != nil {
		t.Fatalf("alice SetLongTerm: %v", err)
	}
	if err := bob.SetLongTerm(ctx, "secret", "bob-secret"); err != nil {
		t.Fatalf("bob SetLongTerm: %v", err)
	}
	if err := global.SetLongTerm(ctx, "secret", "global-secret"); err != nil {
		t.Fatalf("global SetLongTerm: %v", err)
	}

	tests := []struct {
		name  string
		store *Store
		want  string
	}{
		{name: "alice", store: alice, want: "alice-secret"},
		{name: "bob", store: bob, want: "bob-secret"},
		{name: "global", store: global, want: "global-secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each tenant sees exactly one long-term record, and it is its own.
			recs, err := tt.store.ListLongTerm(ctx)
			if err != nil {
				t.Fatalf("ListLongTerm: %v", err)
			}
			if len(recs) != 1 {
				t.Fatalf("expected exactly 1 record for %s, got %d: %+v", tt.name, len(recs), recs)
			}
			if recs[0].Key != "secret" {
				t.Errorf("logical key = %q, want %q", recs[0].Key, "secret")
			}
			if recs[0].Value != tt.want {
				t.Errorf("value = %v, want %v", recs[0].Value, tt.want)
			}

			// Get must resolve the tenant's own value, never a sibling's.
			got, err := tt.store.Get(ctx, "secret")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got != tt.want {
				t.Errorf("Get value = %v, want %v", got, tt.want)
			}
		})
	}

	// Deleting one tenant's memory must not affect the others.
	if err := alice.DeleteLongTerm(ctx, "secret"); err != nil {
		t.Fatalf("alice DeleteLongTerm: %v", err)
	}
	if recs, _ := alice.ListLongTerm(ctx); len(recs) != 0 {
		t.Errorf("alice should have 0 records after delete, got %d", len(recs))
	}
	if recs, _ := bob.ListLongTerm(ctx); len(recs) != 1 {
		t.Errorf("bob should still have 1 record after alice delete, got %d", len(recs))
	}
	if recs, _ := global.ListLongTerm(ctx); len(recs) != 1 {
		t.Errorf("global should still have 1 record after alice delete, got %d", len(recs))
	}
}

// TestLongTerm_TenantIsolation_DelimiterInjection is the adversarial companion
// to TestLongTerm_TenantIsolation. It proves the tokenized namespacing is
// collision-free even when userID or key contains the "::" delimiter, or a
// tenant is named like the old global sentinel. A naive "userID::key" scheme
// leaked here: (userID="a::b", key="c") and (userID="a", key="b::c") both
// produced the stored key "a::b::c", so one tenant could read another's memory.
func TestLongTerm_TenantIsolation_DelimiterInjection(t *testing.T) {
	backend := newMemStorage()
	ctx := context.Background()

	// These two collide under naive concatenation ("a::b"+"::"+"c" == "a"+"::"+"b::c").
	victim := NewStoreForUser("agent1", "a::b", backend)
	attacker := NewStoreForUser("agent1", "a", backend)
	// A tenant literally named like the reserved global bucket must not alias
	// the empty-userID (global/tenantless) bucket.
	named := NewStoreForUser("agent1", "_global_", backend)
	global := NewStore("agent1", backend)

	if err := victim.SetLongTerm(ctx, "c", "victim-secret"); err != nil {
		t.Fatalf("victim SetLongTerm: %v", err)
	}
	if err := named.SetLongTerm(ctx, "x", "named-secret"); err != nil {
		t.Fatalf("named SetLongTerm: %v", err)
	}
	if err := global.SetLongTerm(ctx, "x", "global-secret"); err != nil {
		t.Fatalf("global SetLongTerm: %v", err)
	}

	// Attacker crafts key "b::c" trying to reach victim's ("a::b", "c") record.
	if got, err := attacker.Get(ctx, "b::c"); err == nil {
		t.Fatalf("attacker read %v via crafted key; want an isolation miss", got)
	}
	if recs, _ := attacker.ListLongTerm(ctx); len(recs) != 0 {
		t.Errorf("attacker sees %d long-term records, want 0 (no cross-tenant leak)", len(recs))
	}
	// Victim still reads its own value unaffected.
	if got, err := victim.Get(ctx, "c"); err != nil || got != "victim-secret" {
		t.Errorf("victim Get = %v, %v; want victim-secret", got, err)
	}

	// The "_global_"-named tenant and the real global bucket stay distinct.
	if got, _ := named.Get(ctx, "x"); got != "named-secret" {
		t.Errorf(`tenant "_global_" Get = %v, want named-secret`, got)
	}
	if got, _ := global.Get(ctx, "x"); got != "global-secret" {
		t.Errorf("global bucket Get = %v, want global-secret", got)
	}
	if recs, _ := named.ListLongTerm(ctx); len(recs) != 1 || recs[0].Value != "named-secret" {
		t.Errorf(`tenant "_global_" list = %+v, want exactly its own record`, recs)
	}
	if recs, _ := global.ListLongTerm(ctx); len(recs) != 1 || recs[0].Value != "global-secret" {
		t.Errorf("global bucket list = %+v, want exactly its own record", recs)
	}
}

// TestManager_TenantIsolation verifies that two managers on the same agent but
// different users never see each other's memories, and that WithUserID and the
// empty-userID/global path both behave correctly.
func TestManager_TenantIsolation(t *testing.T) {
	backend := newMemStorage()
	ctx := context.Background()

	aliceMgr := NewManager("agent1", "alice", NewStore("agent1", backend), &mockProvider{})
	bobMgr := NewManager("agent1", "bob", NewStore("agent1", backend), &mockProvider{})
	globalMgr := NewManager("agent1", "", NewStore("agent1", backend), &mockProvider{})

	remember := func(m *Manager, val string) {
		for _, mt := range m.MemoryTools() {
			if mt.Name == "remember" {
				if _, err := mt.Handler(ctx, map[string]any{"key": "fact", "value": val}); err != nil {
					t.Fatalf("remember: %v", err)
				}
			}
		}
	}
	remember(aliceMgr, "alice-fact")
	remember(bobMgr, "bob-fact")
	remember(globalMgr, "global-fact")

	tests := []struct {
		name string
		mgr  *Manager
		want string
	}{
		{name: "alice", mgr: aliceMgr, want: "alice-fact"},
		{name: "bob", mgr: bobMgr, want: "bob-fact"},
		{name: "global", mgr: globalMgr, want: "global-fact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tt.mgr.GetUserMemories(ctx)
			if err != nil {
				t.Fatalf("GetUserMemories: %v", err)
			}
			if !contains(out, tt.want) {
				t.Errorf("memories for %s = %q, want to contain %q", tt.name, out, tt.want)
			}
			// Must not leak sibling tenants' facts.
			for _, other := range tests {
				if other.name == tt.name {
					continue
				}
				if contains(out, other.want) {
					t.Errorf("tenant %s leaked %s's memory: %q", tt.name, other.name, out)
				}
			}
		})
	}

	// WithUserID re-scopes without mutating the original manager.
	reAlice := bobMgr.WithUserID("alice")
	out, err := reAlice.GetUserMemories(ctx)
	if err != nil {
		t.Fatalf("GetUserMemories: %v", err)
	}
	if !contains(out, "alice-fact") || contains(out, "bob-fact") {
		t.Errorf("WithUserID(alice) view = %q; want alice-fact only", out)
	}
	// Original bob manager unchanged.
	if bobMgr.UserID() != "bob" {
		t.Errorf("bobMgr.UserID() = %q, want bob (WithUserID mutated original)", bobMgr.UserID())
	}
}
