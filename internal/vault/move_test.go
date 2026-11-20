package vault

import (
	"path/filepath"
	"testing"

	"gokeep/internal/dbdoc"
)

// TestAMovedGroupCanGoBackToAnAncestor pins that moving a group to the root, or to
// any group above it, is not refused as moving it into itself; only a group below
// the one moved is a cycle.
func TestAMovedGroupCanGoBackToAnAncestor(t *testing.T) {
	v, err := Create(filepath.Join(t.TempDir(), "move.gkdb"), dbdoc.Key{Password: "pw"}, "Move", DefaultSaveSettings())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	outerID, err := v.CreateGroup(v.RootID(), "Outer", 0)
	if err != nil {
		t.Fatal(err)
	}
	innerID, err := v.CreateGroup(outerID, "Inner", 0)
	if err != nil {
		t.Fatal(err)
	}
	childID, err := v.CreateGroup(innerID, "Child", 0)
	if err != nil {
		t.Fatal(err)
	}

	if err := v.MoveGroup(childID, ""); err != nil {
		t.Fatalf("moving Child to the root: %v", err)
	}
	if got := groupParentOf(t, v, childID); got != v.RootID() {
		t.Fatalf("Child's parent is %q, want the root %q", got, v.RootID())
	}
	if err := v.MoveGroup(innerID, outerID); err != nil {
		t.Fatalf("moving a group to its own parent should be a no-op: %v", err)
	}
	if err := v.MoveGroup(outerID, innerID); err == nil {
		t.Fatal("moving a group into one of its children was accepted")
	}
	if got := groupParentOf(t, v, outerID); got != v.RootID() {
		t.Fatalf("Outer moved after the refused drop: its parent is %q", got)
	}
}

// TestMovingAGroupBesideASiblingReordersTheLevel pins the drag that puts a group
// before or after another: the order is the database's own, and a drop beside a row
// of another level moves the group into that row's parent.
func TestMovingAGroupBesideASiblingReordersTheLevel(t *testing.T) {
	v, err := Create(filepath.Join(t.TempDir(), "order.gkdb"), dbdoc.Key{Password: "pw"}, "Order", DefaultSaveSettings())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	alphaID, err := v.CreateGroup(v.RootID(), "Alpha", 0)
	if err != nil {
		t.Fatal(err)
	}
	betaID, err := v.CreateGroup(v.RootID(), "Beta", 0)
	if err != nil {
		t.Fatal(err)
	}
	gammaID, err := v.CreateGroup(v.RootID(), "Gamma", 0)
	if err != nil {
		t.Fatal(err)
	}

	if err := v.MoveGroupBeside(gammaID, alphaID, false); err != nil {
		t.Fatalf("moving Gamma before Alpha: %v", err)
	}
	requireOrder(t, v, "Gamma", "Alpha", "Beta")

	if err := v.MoveGroupBeside(gammaID, betaID, true); err != nil {
		t.Fatalf("moving Gamma after Beta: %v", err)
	}
	requireOrder(t, v, "Alpha", "Beta", "Gamma")

	deltaID, err := v.CreateGroup(betaID, "Delta", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.MoveGroupBeside(deltaID, gammaID, true); err != nil {
		t.Fatalf("moving Delta beside a row of the root: %v", err)
	}
	requireOrder(t, v, "Alpha", "Beta", "Gamma", "Delta")
	if got := groupParentOf(t, v, deltaID); got != v.RootID() {
		t.Fatalf("Delta's parent is %q, want the root", got)
	}

	childID, err := v.CreateGroup(alphaID, "Child", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.MoveGroupBeside(alphaID, childID, true); err == nil {
		t.Fatal("moving a group beside its own child was accepted")
	}
}

// requireOrder fails unless the root's groups are named want, in that order.
func requireOrder(t *testing.T, v *Vault, want ...string) {
	t.Helper()
	groups := v.Tree()
	if len(groups) != len(want) {
		t.Fatalf("the root holds %d groups, want %d", len(groups), len(want))
	}
	for i, name := range want {
		if groups[i].Name != name {
			names := make([]string, 0, len(groups))
			for _, group := range groups {
				names = append(names, group.Name)
			}
			t.Fatalf("the root is ordered %v, want %v", names, want)
		}
	}
}

// groupParentOf is the parent of group id in the tree the interface shows.
func groupParentOf(t *testing.T, v *Vault, id string) string {
	t.Helper()
	var find func(groups []GroupDTO) (string, bool)
	find = func(groups []GroupDTO) (string, bool) {
		for _, group := range groups {
			if group.ID == id {
				return group.ParentID, true
			}
			if parent, ok := find(group.Children); ok {
				return parent, true
			}
		}
		return "", false
	}
	if parent, ok := find(v.Tree()); ok {
		return parent
	}
	t.Fatalf("group %q is not in the tree", id)
	return ""
}
