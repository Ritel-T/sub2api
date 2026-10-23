package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var qqNumber = regexp.MustCompile(`^[1-9][0-9]{4,19}$`)
var qqMessageID = regexp.MustCompile(`^-?[0-9]{1,20}$`)
var qqCode = regexp.MustCompile(`^[A-F0-9]{16}$`)
var ErrQQRequest = errors.New("QQ request unavailable, expired or conflicting")

type QQNotificationService struct {
	db                           *sql.DB
	endpoint, token, secret, bot string
	client                       *http.Client
}

func NewQQNotificationService(db *sql.DB) *QQNotificationService {
	return &QQNotificationService{db: db, endpoint: strings.TrimRight(os.Getenv("QQ_ONEBOT_URL"), "/"), token: os.Getenv("QQ_ONEBOT_TOKEN"), secret: os.Getenv("QQ_WEBHOOK_SECRET"), bot: os.Getenv("QQ_BOT_ID"), client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (s *QQNotificationService) Enabled() bool {
	if s == nil || s.db == nil || len(s.secret) < 32 || s.token == "" || !qqNumber.MatchString(s.bot) {
		return false
	}
	u, e := url.Parse(s.endpoint)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func qqMask(q string) string {
	if len(q) < 5 {
		return ""
	}
	return q[:2] + "****" + q[len(q)-2:]
}
func qqHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }

type QQPreferences struct {
	Channel            string `json:"channel"`
	BalanceLow         bool   `json:"balance_low"`
	SubscriptionExpiry bool   `json:"subscription_expiry"`
}
type QQStatus struct {
	Enabled bool    `json:"enabled"`
	BotQQ   string  `json:"bot_qq"`
	QQ      *string `json:"qq"`
	QQPreferences
	BindingID string     `json:"binding_id"`
	PendingQQ *string    `json:"pending_qq"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (s *QQNotificationService) Status(ctx context.Context, uid int64) (QQStatus, error) {
	out := QQStatus{Enabled: s.Enabled(), QQPreferences: QQPreferences{"email", true, true}}
	if s.Enabled() {
		out.BotQQ = s.bot
	}
	var q, p, code sql.NullString
	var expiry sql.NullTime
	err := s.db.QueryRowContext(ctx, "SELECT qq,channel,balance_low,subscription_expiry,pending_qq,expires_at,code_hash FROM qq_user_notifications WHERE user_id=$1", uid).Scan(&q, &out.Channel, &out.BalanceLow, &out.SubscriptionExpiry, &p, &expiry, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if q.Valid {
		v := qqMask(q.String)
		out.QQ = &v
	}
	if expiry.Valid && expiry.Time.After(time.Now()) {
		out.ExpiresAt = &expiry.Time
		out.BindingID = code.String
		if p.Valid {
			v := qqMask(p.String)
			out.PendingQQ = &v
		}
	}
	return out, nil
}
func (s *QQNotificationService) Issue(ctx context.Context, uid int64) (map[string]any, error) {
	if !s.Enabled() {
		return nil, ErrQQRequest
	}
	b := make([]byte, 8)
	if _, e := rand.Read(b); e != nil {
		return nil, e
	}
	code := strings.ToUpper(hex.EncodeToString(b))
	expiry := time.Now().Add(10 * time.Minute)
	r, e := s.db.ExecContext(ctx, `INSERT INTO qq_user_notifications(user_id,code_hash,expires_at,issued_at) VALUES($1,$2,$3,NOW())
 ON CONFLICT(user_id) DO UPDATE SET code_hash=$2,pending_qq=NULL,expires_at=$3,issued_at=NOW()
 WHERE qq_user_notifications.qq IS NULL AND (qq_user_notifications.issued_at IS NULL OR qq_user_notifications.issued_at < NOW()-INTERVAL '60 seconds')`, uid, qqHash(code), expiry)
	if e != nil {
		return nil, e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return nil, ErrQQRequest
	}
	return map[string]any{"code": code, "expires_at": expiry, "bot_qq": s.bot}, nil
}
func (s *QQNotificationService) Confirm(ctx context.Context, uid int64, masked, bindingID string) error {
	if !s.Enabled() {
		return ErrQQRequest
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var q string
	e = tx.QueryRowContext(ctx, "SELECT pending_qq FROM qq_user_notifications WHERE user_id=$1 AND expires_at>NOW() AND pending_qq IS NOT NULL AND code_hash=$2 FOR UPDATE", uid, bindingID).Scan(&q)
	if e != nil || qqMask(q) != masked {
		return ErrQQRequest
	}
	// Unique qq constraint prevents two users confirming the same account.
	_, e = tx.ExecContext(ctx, "UPDATE qq_user_notifications SET qq=$2,code_hash=NULL,pending_qq=NULL,expires_at=NULL WHERE user_id=$1", uid, q)
	if e != nil {
		return ErrQQRequest
	}
	return tx.Commit()
}
func (s *QQNotificationService) Unbind(ctx context.Context, uid int64) error {
	_, e := s.db.ExecContext(ctx, "UPDATE qq_user_notifications SET qq=NULL,channel='email',code_hash=NULL,pending_qq=NULL,expires_at=NULL WHERE user_id=$1", uid)
	return e
}
func (s *QQNotificationService) Preferences(ctx context.Context, uid int64, p QQPreferences) error {
	if p.Channel != "email" && p.Channel != "qq" && p.Channel != "both" {
		return ErrQQRequest
	}
	if p.Channel != "email" && !s.Enabled() {
		return ErrQQRequest
	}
	var r sql.Result
	var e error
	if p.Channel == "email" {
		r, e = s.db.ExecContext(ctx, `INSERT INTO qq_user_notifications(user_id,channel,balance_low,subscription_expiry) VALUES($1,$2,$3,$4) ON CONFLICT(user_id) DO UPDATE SET channel=$2,balance_low=$3,subscription_expiry=$4`, uid, p.Channel, p.BalanceLow, p.SubscriptionExpiry)
	} else {
		r, e = s.db.ExecContext(ctx, "UPDATE qq_user_notifications SET channel=$2,balance_low=$3,subscription_expiry=$4 WHERE user_id=$1 AND qq IS NOT NULL", uid, p.Channel, p.BalanceLow, p.SubscriptionExpiry)
	}
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrQQRequest
	}
	return nil
}

type QQEvent struct {
	SelfID      json.Number     `json:"self_id"`
	UserID      json.Number     `json:"user_id"`
	MessageID   json.Number     `json:"message_id"`
	Time        int64           `json:"time"`
	PostType    string          `json:"post_type"`
	MessageType string          `json:"message_type"`
	SubType     string          `json:"sub_type"`
	Message     json.RawMessage `json:"message"`
}

func qqText(raw json.RawMessage) (string, bool) {
	var segments []struct {
		Type string `json:"type"`
		Data struct {
			Text string `json:"text"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &segments) != nil || len(segments) != 1 || segments[0].Type != "text" {
		return "", false
	}
	t := strings.TrimSpace(segments[0].Data.Text)
	if len(t) > 100 || strings.ContainsAny(t, "\r\n") || strings.Contains(t, "[CQ:") {
		return "", false
	}
	return t, true
}
func (s *QQNotificationService) Authenticate(body []byte, signature string) bool {
	if !s.Enabled() {
		return false
	}
	expected := hmac.New(sha256.New, []byte(s.secret))
	expected.Write(body)
	got, e := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	return e == nil && strings.HasPrefix(signature, "sha256=") && hmac.Equal(got, expected.Sum(nil))
}
func (s *QQNotificationService) Webhook(ctx context.Context, body []byte) error {
	var ev QQEvent
	if json.Unmarshal(body, &ev) != nil {
		return ErrQQRequest
	}
	if ev.SelfID.String() != s.bot || !qqNumber.MatchString(ev.UserID.String()) || ev.UserID.String() == s.bot || ev.PostType != "message" || ev.MessageType != "private" || ev.SubType != "friend" || !qqMessageID.MatchString(ev.MessageID.String()) || time.Since(time.Unix(ev.Time, 0)).Abs() > 5*time.Minute {
		return ErrQQRequest
	}
	text, ok := qqText(ev.Message)
	if !ok {
		return nil
	}
	if text != "/sub 余额" && text != "/sub 套餐" && text != "/sub 今日用量" && !strings.HasPrefix(text, "/sub 绑定 ") {
		return nil
	}
	if len(ev.MessageID.String()) > 30 {
		return ErrQQRequest
	}
	// Per-sender transaction lock serializes replay and quota checks across replicas.
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "qq:"+ev.UserID.String()); e != nil {
		return e
	}
	var count int
	e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM qq_inbound_events WHERE sender=$1 AND created_at>NOW()-INTERVAL '1 minute'", ev.UserID.String()).Scan(&count)
	if e != nil {
		return e
	}
	if count >= 10 {
		return ErrQQRequest
	}
	key := ev.SelfID.String() + ":" + ev.UserID.String() + ":" + ev.MessageID.String()
	r, e := tx.ExecContext(ctx, "INSERT INTO qq_inbound_events(event_key,sender) VALUES($1,$2) ON CONFLICT DO NOTHING", key, ev.UserID.String())
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return nil
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	reply := ""
	var boundUID int64
	if strings.HasPrefix(text, "/sub 绑定 ") {
		code := strings.TrimPrefix(text, "/sub 绑定 ")
		if !qqCode.MatchString(code) {
			return nil
		}
		r, e = s.db.ExecContext(ctx, `UPDATE qq_user_notifications SET pending_qq=$2 WHERE code_hash=$1 AND expires_at>NOW() AND pending_qq IS NULL
 AND NOT EXISTS(SELECT 1 FROM qq_user_notifications q WHERE q.qq=$2 AND q.user_id<>qq_user_notifications.user_id)`, qqHash(code), ev.UserID.String())
		if e != nil {
			return e
		}
		n, _ = r.RowsAffected()
		if n == 1 {
			reply = "已收到绑定申请，请返回 Sub2API 控制台核对 QQ 后确认。"
		} else {
			reply = "绑定码无效、已使用或已过期，请返回控制台重新生成。"
		}
	} else {
		var uid int64
		e = s.db.QueryRowContext(ctx, "SELECT n.user_id FROM qq_user_notifications n JOIN users u ON u.id=n.user_id WHERE n.qq=$1 AND u.deleted_at IS NULL AND u.status='active'", ev.UserID.String()).Scan(&uid)
		if errors.Is(e, sql.ErrNoRows) {
			reply = "请先在 Sub2API 控制台绑定 QQ。"
		} else if e != nil {
			return e
		} else {
			boundUID = uid
			reply, e = s.query(ctx, uid, text)
			if e != nil {
				return e
			}
		}
	}
	return s.deliver(ctx, boundUID, ev.UserID.String(), "reply:"+key, reply, boundUID > 0)
}
func (s *QQNotificationService) query(ctx context.Context, uid int64, command string) (string, error) {
	switch command {
	case "/sub 余额":
		var v float64
		e := s.db.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active'", uid).Scan(&v)
		return fmt.Sprintf("当前余额：$%.4f", v), e
	case "/sub 今日用量":
		var n int64
		var v float64
		start := time.Now().UTC().Truncate(24 * time.Hour)
		e := s.db.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(actual_cost),0) FROM usage_logs WHERE user_id=$1 AND created_at >= $2", uid, start).Scan(&n, &v)
		return fmt.Sprintf("今日用量（UTC）：%d 次请求，实际费用 $%.4f", n, v), e
	case "/sub 套餐":
		rows, e := s.db.QueryContext(ctx, "SELECT g.name,s.expires_at FROM user_subscriptions s JOIN groups g ON g.id=s.group_id WHERE s.user_id=$1 AND s.deleted_at IS NULL AND s.status='active' AND s.expires_at>NOW() ORDER BY s.expires_at LIMIT 10", uid)
		if e != nil {
			return "", e
		}
		defer rows.Close()
		lines := []string{}
		for rows.Next() {
			var name string
			var expiry time.Time
			if e = rows.Scan(&name, &expiry); e != nil {
				return "", e
			}
			lines = append(lines, fmt.Sprintf("%s：%s UTC 到期", name, expiry.UTC().Format("2006-01-02 15:04")))
		}
		if len(lines) == 0 {
			return "暂无有效套餐。", rows.Err()
		}
		return strings.Join(lines, "\n"), rows.Err()
	}
	return "", ErrQQRequest
}

// Route only the two explicitly supported business events. Disabled transport preserves email.
func (s *QQNotificationService) Route(ctx context.Context, input NotificationEmailSendInput) (bool, error) {
	if !s.Enabled() || input.UserID <= 0 || (input.Event != NotificationEmailEventBalanceLow && input.Event != NotificationEmailEventSubscriptionExpiryReminder) {
		return true, nil
	}
	var q sql.NullString
	var p QQPreferences
	e := s.db.QueryRowContext(ctx, "SELECT qq,channel,balance_low,subscription_expiry FROM qq_user_notifications WHERE user_id=$1", input.UserID).Scan(&q, &p.Channel, &p.BalanceLow, &p.SubscriptionExpiry)
	if errors.Is(e, sql.ErrNoRows) {
		return true, nil
	}
	if e != nil {
		return true, e
	}
	enabled := p.BalanceLow
	if input.Event == NotificationEmailEventSubscriptionExpiryReminder {
		enabled = p.SubscriptionExpiry
	}
	if !enabled {
		return false, nil
	}
	if p.Channel == "email" || !q.Valid {
		return true, nil
	}
	var message string
	if input.Event == NotificationEmailEventBalanceLow {
		message = "Sub2API 余额不足提醒：当前余额 $" + input.Variables["current_balance"] + "，阈值 $" + input.Variables["threshold"] + "。请登录控制台查看。"
	} else {
		message = "Sub2API 套餐到期提醒：" + input.Variables["subscription_group"] + "，" + input.Variables["days_remaining"] + " 天后到期。请登录控制台查看。"
	}
	key := "notice:" + qqHash(fmt.Sprintf("%d|%s|%s|%s|%s", input.UserID, input.Event, input.SourceType, input.SourceID, input.ReminderKey))
	e = s.deliver(ctx, input.UserID, q.String, key, message, true, input.Event)
	return p.Channel != "qq", e
}
func (s *QQNotificationService) deliver(ctx context.Context, uid int64, q, key, message string, checkBinding bool, event ...string) error {
	if message == "" {
		return nil
	}
	// Keep the binding row locked through dispatch, so completed unbind cannot race a later send.
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if checkBinding {
		var actual string
		e = tx.QueryRowContext(ctx, "SELECT qq FROM qq_user_notifications WHERE user_id=$1 FOR UPDATE", uid).Scan(&actual)
		if e != nil || actual != q {
			return ErrQQRequest
		}
	}
	r, e := tx.ExecContext(ctx, "INSERT INTO qq_delivery_attempts(delivery_key,user_id,qq,state) VALUES($1,NULLIF($2,0),$3,'attempting') ON CONFLICT DO NOTHING", key, uid, q)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		var state string
		if e = tx.QueryRowContext(ctx, "SELECT state FROM qq_delivery_attempts WHERE delivery_key=$1", key).Scan(&state); e != nil {
			return e
		}
		if state != "sent" {
			return errors.New("QQ earlier attempt unresolved; retry suppressed")
		}
		return nil
	}
	// Commit the attempt BEFORE network I/O. Never retry unknown or rejected sends automatically.
	if e = tx.Commit(); e != nil {
		return e
	}
	if checkBinding {
		tx, e = s.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var actual string
		e = tx.QueryRowContext(ctx, "SELECT qq FROM qq_user_notifications WHERE user_id=$1 FOR UPDATE", uid).Scan(&actual)
		if e != nil || actual != q {
			return ErrQQRequest
		}

		var active bool
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active')", uid).Scan(&active); e != nil {
			return e
		}
		if !active {
			return ErrQQRequest
		}
		if len(event) > 0 {
			var channel string
			var balance, expiry bool
			if e = tx.QueryRowContext(ctx, "SELECT channel,balance_low,subscription_expiry FROM qq_user_notifications WHERE user_id=$1", uid).Scan(&channel, &balance, &expiry); e != nil {
				return e
			}
			enabled := balance
			if event[0] == NotificationEmailEventSubscriptionExpiryReminder {
				enabled = expiry
			}
			if channel == "email" || !enabled {
				return ErrQQRequest
			}
		}
	}
	payload, _ := json.Marshal(map[string]any{"user_id": json.Number(q), "message": message, "auto_escape": true})
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/send_private_msg", bytes.NewReader(payload))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	res, e := s.client.Do(req)
	state := "unknown"
	messageID := ""
	if e == nil {
		defer res.Body.Close()
		var result struct {
			Status  string `json:"status"`
			RetCode int    `json:"retcode"`
			Data    struct {
				MessageID json.Number `json:"message_id"`
			} `json:"data"`
		}
		data, readErr := io.ReadAll(io.LimitReader(res.Body, 65537))
		if readErr == nil && len(data) <= 65536 && json.Unmarshal(data, &result) == nil && res.StatusCode == 200 && result.Status == "ok" && result.RetCode == 0 && result.Data.MessageID.String() != "" {
			state = "sent"
			messageID = result.Data.MessageID.String()
		} else if readErr == nil && result.Status == "failed" {
			state = "rejected"
		}
	}
	if checkBinding {
		_ = tx.Commit()
	}
	finalCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, updateErr := s.db.ExecContext(finalCtx, "UPDATE qq_delivery_attempts SET state=$2,finished_at=NOW(),message_id=NULLIF($3,'') WHERE delivery_key=$1", key, state, messageID)
	if updateErr != nil {
		return updateErr
	}
	if state != "sent" {
		return errors.New("QQ delivery outcome " + state + "; automatic retry suppressed")
	}
	return nil
}
func ProvideNotificationEmailWithQQ(settings SettingRepository, email *EmailService, qq *QQNotificationService) *NotificationEmailService {
	svc := NewNotificationEmailService(settings, email)
	svc.qq = qq
	return svc
}
