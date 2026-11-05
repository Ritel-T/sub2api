//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestOAuthDedicatedProxySelectionTransactionSeesBindingsAndSerializes(t *testing.T) {
	db := scopedAdminKeyTestDB(t)
	_, err := db.Exec(`CREATE TABLE proxies(
 id BIGINT PRIMARY KEY,name TEXT NOT NULL DEFAULT '',protocol TEXT NOT NULL DEFAULT 'http',host TEXT NOT NULL DEFAULT '192.0.2.1',port INT NOT NULL DEFAULT 8080,
 mihomo_node_id TEXT,username TEXT,password TEXT,status TEXT NOT NULL DEFAULT 'active',expires_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),fallback_mode TEXT NOT NULL DEFAULT 'none',backup_proxy_id BIGINT,expiry_warn_days INT NOT NULL DEFAULT 7);
 CREATE TABLE accounts(id BIGINT PRIMARY KEY,proxy_id BIGINT,deleted_at TIMESTAMPTZ);
 INSERT INTO proxies(id) VALUES(1),(2),(3);
 UPDATE proxies SET status='inactive' WHERE id=2;
 UPDATE proxies SET deleted_at=NOW() WHERE id=3;
 INSERT INTO accounts VALUES(1,1,NULL),(2,2,NOW());`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := newProxyRepositoryWithSQL(client, db)
	require.Error(t, repo.LockAccountProxySelection(context.Background()))
	_, _, err = repo.ListAccountProxySelection(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 100})
	require.Error(t, err)
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := dbent.NewTxContext(context.Background(), tx)
	require.NoError(t, repo.LockAccountProxySelection(ctx))
	items, page, err := repo.ListAccountProxySelection(ctx, pagination.PaginationParams{Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Len(t, items, 1)
	require.EqualValues(t, 1, items[0].AccountCount)
	items, _, err = repo.ListAccountProxySelection(ctx, pagination.PaginationParams{Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "inactive", items[0].Status)
	require.Zero(t, items[0].AccountCount)
	_, err = tx.Client().ExecContext(ctx, `INSERT INTO accounts VALUES(3,2,NULL)`)
	require.NoError(t, err)
	items, _, err = repo.ListAccountProxySelection(ctx, pagination.PaginationParams{Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 1, items[0].AccountCount, "same-transaction batch bindings must be visible")
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		other, err := client.Tx(context.Background())
		if err != nil {
			done <- err
			return
		}
		defer other.Rollback()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c = dbent.NewTxContext(c, other)
		close(started)
		if err := repo.LockAccountProxySelection(c); err != nil {
			done <- err
			return
		}
		rows, _, err := repo.ListAccountProxySelection(c, pagination.PaginationParams{Page: 2, PageSize: 1})
		if err == nil && (len(rows) != 1 || rows[0].AccountCount != 1) {
			t.Error("committed binding was not observed")
		}
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("selection crossed an uncommitted binding: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	require.NoError(t, tx.Commit())
	require.NoError(t, <-done)
}
