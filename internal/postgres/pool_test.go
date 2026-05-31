package postgres

import (
	"database/sql"
	"testing"
)

func TestConfigureDBPoolUsesExplicitProductionDefaults(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://fulfillhub:postgres@localhost:5432/fulfillhub_test?sslmode=disable")
	if err != nil {
		t.Fatalf("open db handle: %v", err)
	}
	defer db.Close()

	configureDBPool(db)

	stats := db.Stats()
	if stats.MaxOpenConnections != defaultMaxOpenConns {
		t.Fatalf("MaxOpenConnections = %d, want %d", stats.MaxOpenConnections, defaultMaxOpenConns)
	}
}
