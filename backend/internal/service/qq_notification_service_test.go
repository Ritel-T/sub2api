package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func qqTestService(t *testing.T) (*QQNotificationService, sqlmock.Sqlmock) {
	t.Helper()
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Close()
		if e := m.ExpectationsWereMet(); e != nil {
			t.Error(e)
		}
	})
	return &QQNotificationService{db: db, endpoint: "http://127.0.0.1", secret: strings.Repeat("x", 32), token: "test", bot: "123456", client: &http.Client{Timeout: time.Second}}, m
}
func TestQQAuthenticationAndTextBoundary(t *testing.T) {
	s, _ := qqTestService(t)
	body := []byte("{}")
	h := hmac.New(sha256.New, []byte(s.secret))
	h.Write(body)
	sig := "sha256=" + hex.EncodeToString(h.Sum(nil))
	if !s.Authenticate(body, sig) || s.Authenticate([]byte("{ }"), sig) || s.Authenticate(body, "sha256=00") {
		t.Fatal("HMAC boundary")
	}
	for _, raw := range []string{`" /sub 余额 "`, `[{"type":"image","data":{"text":"/sub 余额"}}]`, `[{"type":"text","data":{"text":"/sub 余额"}},{"type":"text","data":{"text":"x"}}]`, `[{"type":"text","data":{"text":"[CQ:at,qq=1]"}}]`} {
		if _, ok := qqText(json.RawMessage(raw)); ok {
			t.Fatalf("accepted %s", raw)
		}
	}
	if text, ok := qqText(json.RawMessage(`[{"type":"text","data":{"text":"/sub 余额"}}]`)); !ok || text != "/sub 余额" {
		t.Fatal("valid text")
	}
}
func TestQQRejectsCrossChannelAndExpiredEventsBeforeDB(t *testing.T) {
	s, _ := qqTestService(t)
	for _, ev := range []QQEvent{
		{SelfID: "999999", UserID: "123457", MessageID: "1", Time: time.Now().Unix(), PostType: "message", MessageType: "private", SubType: "friend"},
		{SelfID: "123456", UserID: "123457", MessageID: "1", Time: time.Now().Unix(), PostType: "message", MessageType: "group", SubType: "friend"},
		{SelfID: "123456", UserID: "123457", MessageID: "1", Time: time.Now().Add(-6 * time.Minute).Unix(), PostType: "message", MessageType: "private", SubType: "friend"},
		{SelfID: "123456", UserID: "123457", MessageID: "1", Time: time.Now().Unix(), PostType: "message", MessageType: "private", SubType: "group"},
	} {
		b, _ := json.Marshal(ev)
		if !errors.Is(s.Webhook(context.Background(), b), ErrQQRequest) {
			t.Fatal("accepted invalid event")
		}
	}
}
func TestQQConfirmationBoundToOwnerAndChallenge(t *testing.T) {
	s, m := qqTestService(t)
	m.ExpectBegin()
	m.ExpectQuery("SELECT pending_qq").WithArgs(int64(42), "old-challenge").WillReturnError(sql.ErrNoRows)
	m.ExpectRollback()
	if s.Confirm(context.Background(), 42, "12****78", "old-challenge") == nil {
		t.Fatal("stale confirmation accepted")
	}
}
func TestQQUnknownAttemptNeverResends(t *testing.T) {
	s, m := qqTestService(t)
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer server.Close()
	s.endpoint = server.URL
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO qq_delivery_attempts").WithArgs("dedup", int64(0), "123457").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT state").WithArgs("dedup").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow("unknown"))
	m.ExpectRollback()
	if s.deliver(context.Background(), 0, "123457", "dedup", "private", false) == nil || hits != 0 {
		t.Fatal("unknown resent or reported successful")
	}
}
func TestQQUnbindPreventsDelivery(t *testing.T) {
	s, m := qqTestService(t)
	m.ExpectBegin()
	m.ExpectQuery("SELECT qq").WithArgs(int64(42)).WillReturnError(sql.ErrNoRows)
	m.ExpectRollback()
	if s.deliver(context.Background(), 42, "123457", "notice", "private", true) == nil {
		t.Fatal("sent after unbind")
	}
}
func TestQQDefaultAndAuthEmailUnchanged(t *testing.T) {
	s, m := qqTestService(t)
	if email, e := s.Route(context.Background(), NotificationEmailSendInput{UserID: 42, Event: NotificationEmailEventAuthPasswordReset}); !email || e != nil {
		t.Fatal("auth channel changed")
	}
	m.ExpectQuery("SELECT qq,channel").WithArgs(int64(42)).WillReturnError(sql.ErrNoRows)
	if email, e := s.Route(context.Background(), NotificationEmailSendInput{UserID: 42, Event: NotificationEmailEventBalanceLow}); !email || e != nil {
		t.Fatal("default channel changed")
	}
}

func TestQQBareSubHelpWithoutAccountAccess(t *testing.T) {
	s, m := qqTestService(t)
	var reply string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Message string `json:"message"`
		}
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			t.Error(e)
		}
		reply = payload.Message
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","retcode":0,"data":{"message_id":123}}`))
	}))
	defer server.Close()
	s.endpoint = server.URL
	// Only event/rate-limit and delivery audit SQL is permitted; account queries fail the mock.
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("qq:234567").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT COUNT").WithArgs("234567").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectExec("INSERT INTO qq_inbound_events").WithArgs("123456:234567:9", "234567").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO qq_delivery_attempts").WithArgs("reply:123456:234567:9", int64(0), "234567").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectExec("UPDATE qq_delivery_attempts").WithArgs("reply:123456:234567:9", "sent", "123").WillReturnResult(sqlmock.NewResult(0, 1))
	ev := QQEvent{SelfID: "123456", UserID: "234567", MessageID: "9", Time: time.Now().Unix(), PostType: "message", MessageType: "private", SubType: "friend", Message: json.RawMessage(`[{"type":"text","data":{"text":"/sub"}}]`)}
	body, _ := json.Marshal(ev)
	if e := s.Webhook(context.Background(), body); e != nil {
		t.Fatal(e)
	}
	for _, text := range []string{"控制台", "QQ 通知", "/sub 绑定", "/sub 余额", "/sub 套餐", "/sub 今日用量"} {
		if !strings.Contains(reply, text) {
			t.Errorf("missing help text %s", text)
		}
	}
	if strings.Contains(reply, "当前余额") || strings.Contains(reply, "实际费用") {
		t.Fatal("help contains account information")
	}
}
