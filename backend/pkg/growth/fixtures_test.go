//go:build postgres

package growth

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pentagi/migrations"
	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"

	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

// openSchema fails rather than skips without a server: a skip would read as coverage the shipped SQL does not have.
func openSchema(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("PENTAGI_TEST_DSN")
	if dsn == "" {
		t.Fatal("PENTAGI_TEST_DSN is unset; this tier needs a postgres to prove anything")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// search_path is session state, and a pool could hand a later query to a connection that never received it.
	db.SetMaxOpenConns(1)

	schema := fmt.Sprintf("growth_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	if _, err := db.Exec("SET search_path TO " + schema + ", public"); err != nil {
		t.Fatalf("search_path: %v", err)
	}

	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrations.EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	goose.SetTableName(schema + ".goose_db_version")
	if err := goose.Up(db, "sql"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type world struct {
	db   *sql.DB
	svc  *Service
	tg   *MemoryConnector
	user int64
}

// newWorld is one customer with an active Telegram account bound to an in-memory platform.
func newWorld(t *testing.T) *world {
	t.Helper()
	db := openSchema(t)
	jev := httptest.NewServer(http.HandlerFunc(keepEveryMessage))
	t.Cleanup(jev.Close)
	svc := NewService(database.New(db), &config.Config{
		GrowthSecretKey: "test-master-key", SMTPPort: 587, SMTPTLS: smtpStartTLS,
		TypeSafeAPIKey: "ts-test", TypeSafeAPIURL: jev.URL,
	})
	t.Cleanup(func() { _ = svc.Close() })

	w := &world{db: db, svc: svc, tg: NewMemoryConnector(connectors.PlatformTelegram), user: seedUser(t, db, "owner@example.test")}
	acc, err := svc.EnsureAccount(context.Background(), w.user, AccountSpec{Platform: connectors.PlatformTelegram})
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	svc.SetConnector(acc.ID, w.tg)
	w.signedInDaysAgo(t, acc.ID, 30)
	return w
}

// gameProfile is a customer with the default weights: 3/3/2/2/3, qualified from 7.
func gameProfile() Profile {
	return Profile{
		Name:          "Starfall Studio",
		Pitch:         " A fair MMORPG with no pay-to-win ",
		Games:         []string{"Starfall Online"},
		Genre:         "MMORPG",
		Competitors:   []string{"Rival Saga"},
		Regions:       []string{"SEA", " "},
		Languages:     []string{"英语", "泰语"},
		DefaultTarget: 50,
		Signals:       []string{"s1", "s2", "s3", "s4", "s5", "s6"},
		Excludes:      []string{"account sellers"},
		Notes:         "Players who quit Rival Saga after the last patch are the best fit.",
	}
}

// judgeAsAGamePublisher saves gameProfile, so scores follow its weights rather than the
// default preset's.
func (w *world) judgeAsAGamePublisher(t *testing.T) {
	t.Helper()
	if _, err := w.svc.SaveProfile(context.Background(), w.user, gameProfile()); err != nil {
		t.Fatalf("save the game profile: %v", err)
	}
}

// keepEveryMessage is a Jev that answers yes to every question.
func keepEveryMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Questions map[string]json.RawMessage `json:"questions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	answers := make(map[string]any, len(body.Questions))
	for qid := range body.Questions {
		answers[qid] = map[string]any{"type": "noul", "noul": 1.0}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers})
}

// signedInDaysAgo backdates an account's first sign-in, which its join pace counts from.
func (w *world) signedInDaysAgo(t *testing.T, accountID int64, days float64) {
	t.Helper()
	at := time.Now().Add(-time.Duration(days * float64(24*time.Hour)))
	if _, err := w.db.Exec(`UPDATE channel_accounts SET first_signed_in_at = $2 WHERE id = $1`, accountID, at); err != nil {
		t.Fatalf("backdate account %d: %v", accountID, err)
	}
}

// savePace saves the Telegram setting with the given pace fields.
func (w *world) savePace(t *testing.T, fields map[string]string) {
	t.Helper()
	values := map[string]string{"api_id": "12345", "api_hash": "0123456789abcdef0123456789abcdef"}
	for k, v := range fields {
		values[k] = v
	}
	if _, err := w.svc.SaveSetting(context.Background(), w.user, SettingTelegram, "", values); err != nil {
		t.Fatalf("save pace: %v", err)
	}
	w.svc.SetConnector(w.firstAccount(t), w.tg)
}

func seedUser(t *testing.T, db *sql.DB, mail string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(`
		INSERT INTO users (hash, mail, name, status, role_id, type, provider)
		VALUES (md5($1), $1, 'growth', 'active', 2, 'local', 'local')
		RETURNING id`, mail).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

// seedTask opens a flow with one task and returns the scope an agent of that task runs in.
func (w *world) seedTask(t *testing.T) Scope {
	t.Helper()
	var flowID int64
	err := w.db.QueryRow(`
		INSERT INTO flows (title, model, model_provider_name, language, user_id, model_provider_type)
		VALUES ('leads', 'm', 'p', 'en', $1, 'openai')
		RETURNING id`, w.user).Scan(&flowID)
	if err != nil {
		t.Fatalf("seed flow: %v", err)
	}
	return w.seedTaskIn(t, flowID)
}

func (w *world) seedTaskIn(t *testing.T, flowID int64) Scope {
	t.Helper()
	var taskID int64
	if err := w.db.QueryRow(`INSERT INTO tasks (input, flow_id) VALUES ('find 50', $1) RETURNING id`, flowID).
		Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return Scope{UserID: w.user, FlowID: flowID, TaskID: &taskID}
}

func tgGroup(id, username, title string) connectors.Source {
	return connectors.Source{
		ExternalID: id,
		Username:   username,
		Title:      title,
		Link:       "https://t.me/" + username,
		Kind:       connectors.SourceKindGroup,
		Peer:       []byte(`{"access_hash":"` + id + `"}`),
	}
}

func tgMessage(id int64, sender, text string) connectors.Message {
	return connectors.Message{
		ID:             id,
		SenderID:       sender,
		SenderUsername: "u" + sender,
		Text:           text,
		Time:           time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		Link:           fmt.Sprintf("https://t.me/c/1/%d", id),
	}
}

// qualified builds a lead input that scores as qualified, with one piece of evidence.
func qualified(userID string, sourceID int64, messageID string) LeadInput {
	return LeadInput{
		PlatformUserID: userID,
		Username:       "u" + userID,
		Score:          9,
		Tier:           TierQualified,
		MatchReason:    "player+3, wants similar game+3, 7d+2, genre partial+1 = 9",
		Evidence: []EvidenceInput{{
			SourceID:    sourceID,
			MessageID:   messageID,
			MessageLink: "https://t.me/c/1/" + messageID,
			Excerpt:     "any game like this one?",
		}},
	}
}

func (w *world) mustSaveQualified(t *testing.T, scope Scope, sourceID int64, firstUser, n int) {
	t.Helper()
	inputs := make([]LeadInput, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprint(firstUser + i)
		inputs = append(inputs, qualified(id, sourceID, "m"+id))
	}
	res, err := w.svc.UpsertLeads(context.Background(), scope, inputs)
	if err != nil {
		t.Fatalf("save leads: %v", err)
	}
	for _, r := range res {
		if r.Status == LeadStatusRejected {
			t.Fatalf("lead %s rejected: %s", r.PlatformUserID, r.Reason)
		}
	}
}

func (w *world) mustAddTelegramGroup(t *testing.T, scope Scope, src connectors.Source, msgs ...connectors.Message) int64 {
	t.Helper()
	w.tg.AddSource(src, msgs...)
	res, err := w.svc.AddSources(context.Background(), scope, []string{src.Link}, OriginManual, nil)
	if err != nil || len(res) != 1 || res[0].Source == nil {
		t.Fatalf("add %s: %v %+v", src.Link, err, res)
	}
	return res[0].Source.ID
}

// addAccount attaches another Telegram account of the world's user, served by its own in-memory platform.
func (w *world) addAccount(t *testing.T, label string) (int64, *MemoryConnector) {
	t.Helper()
	acc, err := w.svc.EnsureAccount(context.Background(), w.user, AccountSpec{Platform: connectors.PlatformTelegram, Label: label})
	if err != nil {
		t.Fatalf("seed account %s: %v", label, err)
	}
	conn := NewMemoryConnector(connectors.PlatformTelegram)
	w.svc.SetConnector(acc.ID, conn)
	return acc.ID, conn
}

func (w *world) firstAccount(t *testing.T) int64 {
	t.Helper()
	accounts, err := w.svc.ListAccounts(context.Background(), w.user)
	if err != nil || len(accounts) == 0 {
		t.Fatalf("list accounts: %v", err)
	}
	return accounts[0].ID
}

type sinkMessage struct {
	From string
	To   []string
	Data string
}

// smtpSink is a plaintext SMTP server that accepts every message; use it with tls "none".
type smtpSink struct {
	Host string
	Port string

	mu   sync.Mutex
	msgs []sinkMessage
}

func startSMTPSink(t *testing.T) *smtpSink {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	sink := &smtpSink{Host: host, Port: port}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go sink.serve(conn)
		}
	}()
	return sink
}

func (s *smtpSink) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	reply := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	reply("220 sink")
	var msg sinkMessage
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			reply("250-sink")
			reply("250 OK")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			msg = sinkMessage{From: strings.Trim(strings.TrimSpace(line)[10:], "<>")}
			reply("250 OK")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			msg.To = append(msg.To, strings.Trim(strings.TrimSpace(line)[8:], "<>"))
			reply("250 OK")
		case cmd == "DATA":
			reply("354 go on")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			msg.Data = data.String()
			s.mu.Lock()
			s.msgs = append(s.msgs, msg)
			s.mu.Unlock()
			reply("250 queued")
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 OK")
		}
	}
}

func (s *smtpSink) Messages() []sinkMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sinkMessage(nil), s.msgs...)
}

// saveSink points the world's user at the sink through the settings store.
func (w *world) saveSink(t *testing.T, sink *smtpSink, notify string) {
	t.Helper()
	_, err := w.svc.SaveSetting(context.Background(), w.user, SettingEmail, "", map[string]string{
		"host": sink.Host, "port": sink.Port, "from": "leads@example.test", "tls": smtpNoTLS, "notify_emails": notify,
	})
	if err != nil {
		t.Fatalf("save email setting: %v", err)
	}
}
