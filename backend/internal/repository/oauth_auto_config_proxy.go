package repository

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Share the existing least-used selection lock until the account binding commits.
func (r *proxyRepository) LockAccountProxySelection(ctx context.Context) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return fmt.Errorf("dedicated proxy selection requires an account transaction")
	}
	_, err := tx.Client().ExecContext(ctx, `SELECT pg_advisory_xact_lock(742319830125)`)
	return err
}

// Selection must see earlier bindings in this same transaction, including
// another row from a batch import; the management listing uses the base client.
func (r *proxyRepository) ListAccountProxySelection(ctx context.Context, params pagination.PaginationParams) ([]service.ProxyWithAccountCount, *pagination.PaginationResult, error) {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return nil, nil, fmt.Errorf("dedicated proxy selection requires an account transaction")
	}
	client := tx.Client()
	total, err := client.Proxy.Query().Where(proxy.DeletedAtIsNil()).Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	items, err := client.Proxy.Query().Where(proxy.DeletedAtIsNil()).Order(dbent.Asc(proxy.FieldID)).Offset(params.Offset()).Limit(params.Limit()).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := client.QueryContext(ctx, `SELECT proxy_id,COUNT(*) FROM accounts WHERE proxy_id IS NOT NULL AND deleted_at IS NULL GROUP BY proxy_id`)
	if err != nil {
		return nil, nil, err
	}
	counts := map[int64]int64{}
	for rows.Next() {
		var id, count int64
		if err := rows.Scan(&id, &count); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		counts[id] = count
	}
	scanErr := rows.Err()
	closeErr := rows.Close()
	if scanErr != nil {
		return nil, nil, scanErr
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	result := make([]service.ProxyWithAccountCount, 0, len(items))
	for _, item := range items {
		p := proxyEntityToService(item)
		result = append(result, service.ProxyWithAccountCount{Proxy: *p, AccountCount: counts[p.ID]})
	}
	return result, paginationResultFromTotal(int64(total), params), nil
}
