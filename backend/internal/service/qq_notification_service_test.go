package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
		_ = db.Close()
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
	_, _ = h.Write(body)
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

func TestQQBoundKeywordHelpPreservesDeliveryChecks(t *testing.T) {
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
	m.ExpectQuery("SELECT n.user_id").WithArgs("234567").WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(42))
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("qq:234567").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT COUNT").WithArgs("234567").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectExec("INSERT INTO qq_inbound_events").WithArgs("123456:234567:9", "234567").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectQuery("SELECT qq").WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"qq"}).AddRow("234567"))
	m.ExpectExec("INSERT INTO qq_delivery_attempts").WithArgs("reply:123456:234567:9", int64(42), "234567").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectQuery("SELECT qq").WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"qq"}).AddRow("234567"))
	m.ExpectQuery("SELECT EXISTS").WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	m.ExpectCommit()
	m.ExpectExec("UPDATE qq_delivery_attempts").WithArgs("reply:123456:234567:9", "sent", "123").WillReturnResult(sqlmock.NewResult(0, 1))
	ev := QQEvent{SelfID: "123456", UserID: "234567", MessageID: "9", Time: time.Now().Unix(), PostType: "message", MessageType: "private", SubType: "friend", Message: json.RawMessage(`[{"type":"text","data":{"text":"帮助"}}]`)}
	body, _ := json.Marshal(ev)
	if e := s.Webhook(context.Background(), body); e != nil {
		t.Fatal(e)
	}
	for _, text := range []string{"账号中心", "验证码", "查余额", "查套餐", "查用量"} {
		if !strings.Contains(reply, text) {
			t.Errorf("missing help text %s", text)
		}
	}
	if strings.Contains(reply, "当前余额") || strings.Contains(reply, "实际费用") {
		t.Fatal("help contains account information")
	}
}

func TestQQKeywordAndBareCodeCommands(t *testing.T) {
	for input, want := range map[string]string{"帮助": "/sub", "查余额": "/sub 余额", "查套餐": "/sub 套餐", "查用量": "/sub 今日用量", "0123456789abcdef": "/sub 绑定 0123456789ABCDEF", "请帮我查余额": "请帮我查余额"} {
		if got := qqCommand(input); got != want {
			t.Errorf("%q: %q", input, got)
		}
	}
}

func TestQQQueryUsesPrivateSenderBindingAndNeverSends(t *testing.T) {
	s, m := qqTestService(t)
	ev := QQEvent{SelfID: "123456", UserID: "234567", MessageID: "10", Time: time.Now().Unix(), PostType: "message", MessageType: "private", SubType: "friend"}
	body, _ := json.Marshal(map[string]any{"event": ev, "query": "balance"})
	signature := func(body []byte) string {
		h := hmac.New(sha256.New, []byte(s.secret))
		_, _ = h.Write(body)
		return "sha256=" + hex.EncodeToString(h.Sum(nil))
	}
	sig := signature(append([]byte("qq-query-v1\n"), body...))
	if !s.AuthenticateQuery(body, sig) || s.Authenticate(body, sig) || s.AuthenticateQuery(body, signature(body)) {
		t.Fatal("query signature crossed endpoint boundary")
	}
	m.ExpectQuery("SELECT n.user_id").WithArgs("234567").WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(42))
	m.ExpectQuery("SELECT balance FROM users").WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(12.3456))
	got, err := s.Query(context.Background(), body)
	if err != nil || got != "当前余额：$12.3456" {
		t.Fatalf("%q %v", got, err)
	}
	ev.MessageType = "group"
	body, _ = json.Marshal(map[string]any{"event": ev, "query": "balance"})
	if _, err = s.Query(context.Background(), body); !errors.Is(err, ErrQQRequest) {
		t.Fatal("group query accepted")
	}
	ev.MessageType = "private"
	body, _ = json.Marshal(map[string]any{"event": ev, "query": "admin"})
	if _, err = s.Query(context.Background(), body); !errors.Is(err, ErrQQRequest) {
		t.Fatal("unsupported query accepted")
	}
}

func TestQQAccessChecksCurrentPrivateBindingOnly(t *testing.T) {
	s, m := qqTestService(t)
	ev := QQEvent{SelfID: "123456", UserID: "234567", MessageID: "11", Time: time.Now().Unix(), PostType: "message", MessageType: "private", SubType: "friend"}
	body, _ := json.Marshal(map[string]any{"event": ev})
	h := hmac.New(sha256.New, []byte(s.secret))
	_, _ = h.Write(append([]byte("qq-access-v1\n"), body...))
	sig := "sha256=" + hex.EncodeToString(h.Sum(nil))
	if !s.AuthenticateAccess(body, sig) || s.AuthenticateQuery(body, sig) || s.Authenticate(body, sig) {
		t.Fatal("access signature crossed endpoint boundary")
	}
	m.ExpectQuery("SELECT n.user_id").WithArgs("234567").WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(42))
	if bound, err := s.Access(context.Background(), body); !bound || err != nil {
		t.Fatalf("bound access: %v %v", bound, err)
	}
	m.ExpectQuery("SELECT n.user_id").WithArgs("234567").WillReturnError(sql.ErrNoRows)
	if bound, err := s.Access(context.Background(), body); bound || err != nil {
		t.Fatalf("unbound access: %v %v", bound, err)
	}
	ev.MessageType = "group"
	body, _ = json.Marshal(map[string]any{"event": ev})
	if _, err := s.Access(context.Background(), body); !errors.Is(err, ErrQQRequest) {
		t.Fatal("group access accepted")
	}
}

func TestQQUnboundCommandsStaySilentWithoutAuditWrites(t *testing.T) {
	s, m := qqTestService(t)
	for _, text := range []string{"帮助", "查余额", "查套餐", "查用量"} {
		m.ExpectQuery("SELECT n.user_id").WithArgs("234567").WillReturnError(sql.ErrNoRows)
		ev := QQEvent{SelfID: "123456", UserID: "234567", MessageID: "12", Time: time.Now().Unix(), PostType: "message", MessageType: "private", SubType: "friend"}
		ev.Message, _ = json.Marshal([]any{map[string]any{"type": "text", "data": map[string]string{"text": text}}})
		body, _ := json.Marshal(ev)
		if err := s.Webhook(context.Background(), body); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}
}

func TestQQUnboundCodeOnlyRecordsPendingWithoutReply(t *testing.T) {
	for _, changed := range []int64{0, 1} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			s, m := qqTestService(t)
			m.ExpectQuery("SELECT n.user_id").WithArgs("234567").WillReturnError(sql.ErrNoRows)
			m.ExpectBegin()
			m.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("qq:234567").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectQuery("SELECT COUNT").WithArgs("234567").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			m.ExpectExec("INSERT INTO qq_inbound_events").WithArgs("123456:234567:13", "234567").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit()
			m.ExpectExec("UPDATE qq_user_notifications SET pending_qq").WithArgs(qqHash("0123456789ABCDEF"), "234567").WillReturnResult(sqlmock.NewResult(0, changed))
			ev := QQEvent{SelfID: "123456", UserID: "234567", MessageID: "13", Time: time.Now().Unix(), PostType: "message", MessageType: "private", SubType: "friend", Message: json.RawMessage(`[{"type":"text","data":{"text":"0123456789ABCDEF"}}]`)}
			body, _ := json.Marshal(ev)
			if err := s.Webhook(context.Background(), body); err != nil {
				t.Fatal(err)
			}
		})
	}
}
