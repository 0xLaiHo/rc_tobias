package platform

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/0xLaiHo/rc_tobias/ent"
	_ "github.com/lib/pq"
)

// OpenDatabase returns both the raw sql.DB and Ent client because outbox claims
// need raw SQL while normal domain writes use Ent.
func OpenDatabase(ctx context.Context, cfg Config) (*sql.DB, *ent.Client, error) {
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	driver := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(driver))
	return db, client, nil
}

// Migrate applies Ent's schema at startup for the self-contained MVP. Multiple
// binaries may start together in Compose, so a PostgreSQL advisory lock
// serializes migration attempts and avoids concurrent type/table creation races.
func Migrate(ctx context.Context, db *sql.DB, client *ent.Client) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	const advisoryLockKey int64 = 42024201
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, advisoryLockKey)

	return client.Schema.Create(ctx)
}
