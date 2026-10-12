package store

import "testing"

func TestPostgresMigrationPlanIsVersioned(t *testing.T) {
	if len(postgresMigrationPlan) == 0 {
		t.Fatal("migration plan is empty")
	}

	for i, migration := range postgresMigrationPlan {
		if migration.Version != i+1 {
			t.Fatalf("migration %d has version %d, want %d", i, migration.Version, i+1)
		}
		if migration.Name == "" {
			t.Fatalf("migration %d has empty name", migration.Version)
		}
		if migration.Run == nil {
			t.Fatalf("migration %d_%s has nil runner", migration.Version, migration.Name)
		}
	}
}
