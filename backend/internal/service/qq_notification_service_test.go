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
