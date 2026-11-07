package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func clientStatusRows(values []driver.Value) *sqlmock.Rows {
	cols := make([]string, len(values))
	for i := range cols {
		cols[i] = fmt.Sprint(i)
	}
	return sqlmock.NewRows(cols).AddRow(values...)
}
func TestOpsErrorLogClientStatusIsDistinctFromEffectiveUpstreamStatus(t *testing.T) {
	for _, client := range []driver.Value{int64(200), int64(502), nil} {
		t.Run(fmt.Sprint(client), func(t *testing.T) {
			db, m, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := NewOpsRepository(db)
			now := time.Now()
			list := []driver.Value{int64(1), now, "upstream", "upstream_error", "provider", "upstream_http", "error", int64(402), client, "anthropic", "test-model", false, nil, nil, "", "client-rid", "rid", "safe-message", nil, "", nil, int64(194), "account", int64(18), "group", nil, "/v1/messages", false, "", "", "test-model", "test-model", "", nil, "", nil}
			m.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
			m.ExpectQuery("(?s)SELECT.*e.status_code,.*FROM ops_error_logs").WithArgs(20, 0).WillReturnRows(clientStatusRows(list))
			got, err := repo.ListErrorLogs(context.Background(), &service.OpsErrorLogFilter{View: "all"})
			require.NoError(t, err)
			require.Len(t, got.Errors, 1)
			require.Equal(t, 402, got.Errors[0].StatusCode)
			if client == nil {
				require.Nil(t, got.Errors[0].ClientStatusCode)
			} else {
				require.Equal(t, int(client.(int64)), *got.Errors[0].ClientStatusCode)
			}
			detail := []driver.Value{int64(1), now, "upstream", "upstream_error", "provider", "upstream_http", "error", int64(402), client, "anthropic", "test-model", false, nil, nil, "client-rid", "rid", "safe-message", "", int64(402), "safe-upstream", "", "", false, nil, "", nil, int64(194), "account", int64(18), "group", nil, "/v1/messages", false, "", "", "test-model", "test-model", nil, "", nil, nil, nil, nil, nil, "", "", nil}
			m.ExpectQuery("(?s)SELECT.*e.status_code,.*FROM ops_error_logs").WithArgs(int64(1)).WillReturnRows(clientStatusRows(detail))
			d, err := repo.GetErrorLogByID(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 402, d.StatusCode)
			require.Equal(t, 402, *d.UpstreamStatusCode)
			require.Equal(t, got.Errors[0].ClientStatusCode, d.ClientStatusCode)
			raw, err := json.Marshal(d)
			require.NoError(t, err)
			require.Contains(t, string(raw), `"client_status_code":`)
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}
