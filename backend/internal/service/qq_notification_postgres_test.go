package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/lib/pq"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQQPostgresBindingAndDelivery(t *testing.T) {
	dsn := os.Getenv("QQ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QQ_TEST_DATABASE_URL required")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	// This DSN must point at an isolated test database; unique schema avoids touching existing tables.
	schema := fmt.Sprintf("qq_test_%d", time.Now().UnixNano())
	if _, e = db.Exec("CREATE SCHEMA " + schema); e != nil {
		t.Fatal(e)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	testDB, e := sql.Open("postgres", dsn+" search_path="+schema)
	if e != nil {
		t.Fatal(e)
	}
	defer testDB.Close()
	_, e = testDB.Exec(`CREATE TABLE users(id BIGINT PRIMARY KEY,balance NUMERIC, status TEXT,deleted_at TIMESTAMPTZ);INSERT INTO users VALUES(1,12.5,'active',NULL),(2,1,'active',NULL);
 CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT);
 CREATE TABLE user_subscriptions(user_id BIGINT,group_id BIGINT,expires_at TIMESTAMPTZ,status TEXT,deleted_at TIMESTAMPTZ);
 CREATE TABLE usage_logs(user_id BIGINT,actual_cost NUMERIC,created_at TIMESTAMPTZ);`)
	if e != nil {
		t.Fatal(e)
	}
	migration, e := os.ReadFile("../../migrations/298_qq_notifications.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = testDB.Exec(string(migration)); e != nil {
		t.Fatal(e)
	}
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok","retcode":0,"data":{"message_id":123}}`)
	}))
	defer server.Close()
	s := &QQNotificationService{db: testDB, endpoint: server.URL, token: "test", secret: strings.Repeat("x", 32), bot: "123456", client: server.Client()}
	ctx := context.Background()
	code, e := s.Issue(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Issue(ctx, 1); e == nil {
		t.Fatal("rate limit absent")
	}
	event := QQEvent{SelfID: "123456", UserID: "234567", MessageID: "1", Time: time.Now().Unix(), PostType: "message", MessageType: "private", SubType: "friend"}
	event.Message = json.RawMessage(fmt.Sprintf(`[{"type":"text","data":{"text":"/sub 绑定 %s"}}]`, code["code"]))
	body, _ := json.Marshal(event)
	if e = s.Webhook(ctx, body); e != nil {
		t.Fatal(e)
	}
	status, e := s.Status(ctx, 1)
	if e != nil || status.PendingQQ == nil {
		t.Fatalf("candidate: %+v %v", status, e)
	}
	if e = s.Confirm(ctx, 2, *status.PendingQQ, status.BindingID); e == nil {
		t.Fatal("cross user confirm")
	}
	if e = s.Confirm(ctx, 1, *status.PendingQQ, "stale"); e == nil {
		t.Fatal("stale confirm")
	}
	if e = s.Confirm(ctx, 1, *status.PendingQQ, status.BindingID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Issue(ctx, 1); e == nil {
		t.Fatal("replace bound account")
	}
	if e = s.Preferences(ctx, 1, QQPreferences{"qq", true, true}); e != nil {
		t.Fatal(e)
	}
	before := hits.Load()
	input := NotificationEmailSendInput{UserID: 1, Event: NotificationEmailEventBalanceLow, SourceID: "1", ReminderKey: "today", Variables: map[string]string{"current_balance": "1"}}
	for i := 0; i < 2; i++ {
		email, e := s.Route(ctx, input)
		if e != nil || email {
			t.Fatalf("QQ route %v %v", email, e)
		}
	}
	if hits.Load() != before+1 {
		t.Fatal("duplicate notification")
	}
	for _, command := range []string{"/sub 余额", "/sub 套餐", "/sub 今日用量"} {
		if _, e = s.query(ctx, 1, command); e != nil {
			t.Fatal(e)
		}
	}

	// Explicit opt-out must be rechecked by delivery, even when a caller previously resolved QQ.
	if e = s.Preferences(ctx, 1, QQPreferences{"email", true, true}); e != nil {
		t.Fatal(e)
	}
	checkpoint := hits.Load()
	if e = s.deliver(ctx, 1, "234567", "stale-preference", "secret", true, NotificationEmailEventBalanceLow); e == nil {
		t.Fatal("ignored latest preference")
	}
	if hits.Load() != checkpoint {
		t.Fatal("sent after preference change")
	}
	if e = s.Preferences(ctx, 1, QQPreferences{"qq", true, true}); e != nil {
		t.Fatal(e)
	}
	// Database uniqueness remains authoritative even if two challenges were claimed before either confirmation.
	_, e = testDB.Exec("INSERT INTO qq_user_notifications(user_id,pending_qq,code_hash,expires_at) VALUES(2,'234567','another-challenge',NOW()+INTERVAL '1 minute')")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Confirm(ctx, 2, qqMask("234567"), "another-challenge"); e == nil {
		t.Fatal("duplicate QQ confirmed")
	}
	if e = s.Unbind(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if e = s.deliver(ctx, 1, "234567", "after-unbind", "secret", true); e == nil {
		t.Fatal("unbound send")
	}
	status, e = s.Status(ctx, 1)
	if e != nil || status.QQ != nil || status.Channel != "email" {
		t.Fatalf("unbind status %+v %v", status, e)
	}
}
