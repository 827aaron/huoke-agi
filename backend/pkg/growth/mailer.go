package growth

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/templates"
)

const (
	smtpStartTLS    = "starttls"
	smtpImplicitTLS = "tls"
	smtpNoTLS       = "none"

	smtpTimeout = 30 * time.Second

	NotifySent    = "sent"
	NotifyFailed  = "failed"
	NotifySkipped = "skipped"
)

var (
	ErrNoSMTP        = errors.New("no SMTP server is configured")
	ErrNoLeadsToSend = errors.New("there are no delivered or qualified leads to send")
)

type attachment struct {
	name        string
	contentType string
	data        []byte
}

func parseRecipients(raw string) ([]string, error) {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addr, err := mail.ParseAddress(part)
		if err != nil {
			return nil, fmt.Errorf("%q is not an email address", part)
		}
		out = append(out, addr.Address)
	}
	return out, nil
}

// smtpClient connects, secures and authenticates; the caller owns Quit.
func smtpClient(ctx context.Context, cfg *config.Config) (*smtp.Client, error) {
	if cfg.SMTPHost == "" {
		return nil, ErrNoSMTP
	}
	addr := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))
	dialer := &net.Dialer{Timeout: smtpTimeout}
	tlsConfig := &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12}

	var (
		conn net.Conn
		err  error
	)
	if cfg.SMTPTLS == smtpImplicitTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", addr, err)
	}
	deadline := time.Now().Add(smtpTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s did not greet as an SMTP server: %w", addr, err)
	}
	if cfg.SMTPTLS != smtpImplicitTLS && cfg.SMTPTLS != smtpNoTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			client.Close()
			return nil, fmt.Errorf("%s does not offer STARTTLS; set tls to %q or %q", addr, smtpImplicitTLS, smtpNoTLS)
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			client.Close()
			return nil, fmt.Errorf("STARTTLS with %s failed: %w", addr, err)
		}
	}
	if cfg.SMTPUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPHost)); err != nil {
			client.Close()
			return nil, fmt.Errorf("login to %s refused: %w", addr, err)
		}
	}
	return client, nil
}

func checkSMTP(ctx context.Context, cfg *config.Config) error {
	client, err := smtpClient(ctx, cfg)
	if err != nil {
		return err
	}
	return client.Quit()
}

func sendMail(ctx context.Context, cfg *config.Config, to []string, subject, body string, files ...attachment) error {
	if len(to) == 0 {
		return errors.New("no recipient")
	}
	from, err := mail.ParseAddress(cfg.SMTPFrom)
	if err != nil {
		return fmt.Errorf("sender %q is not an email address: %w", cfg.SMTPFrom, err)
	}
	msg, err := buildMessage(from, to, subject, body, files)
	if err != nil {
		return err
	}

	client, err := smtpClient(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("sender refused: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("recipient %s refused: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("server refused the message: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("failed to send the message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("server rejected the message: %w", err)
	}
	return client.Quit()
}

func buildMessage(from *mail.Address, to []string, subject, body string, files []attachment) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	header := []string{
		"From: " + from.String(),
		"To: " + strings.Join(to, ", "),
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=" + mw.Boundary(),
	}
	var out bytes.Buffer
	out.WriteString(strings.Join(header, "\r\n") + "\r\n\r\n")

	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return nil, err
	}
	writeBase64(part, []byte(body))

	for _, f := range files {
		part, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {f.contentType},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": f.name})},
		})
		if err != nil {
			return nil, err
		}
		writeBase64(part, f.data)
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	out.Write(buf.Bytes())
	return out.Bytes(), nil
}

// writeBase64 wraps at 76 columns, the line limit mail transports enforce.
func writeBase64(w interface{ Write([]byte) (int, error) }, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		_, _ = w.Write([]byte(enc[:76] + "\r\n"))
		enc = enc[76:]
	}
	_, _ = w.Write([]byte(enc + "\r\n"))
}

// notifyRecipients is the owner's notify_emails setting, or else the owner's login address.
func (s *Service) notifyRecipients(ctx context.Context, userID int64) ([]string, error) {
	row, err := s.db.GetUserGrowthSetting(ctx, database.GetUserGrowthSettingParams{UserID: userID, Kind: SettingEmail})
	if err == nil {
		if stored, err := s.open(row); err == nil {
			if list, err := parseRecipients(stored.Values["notify_emails"]); err == nil && len(list) > 0 {
				return list, nil
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to load email setting: %w", err)
	}

	user, err := s.db.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user %d: %w", userID, err)
	}
	return parseRecipients(user.Mail)
}

// SendTestEmail mails a short message with the configuration the customer's deliveries use.
// to overrides the recipients when non-empty; an empty lang is the customer's Language.
func (s *Service) SendTestEmail(ctx context.Context, userID int64, to, lang string) ([]string, error) {
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	recipients, err := parseRecipients(to)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSetting, err)
	}
	if len(recipients) == 0 {
		if recipients, err = s.notifyRecipients(ctx, userID); err != nil {
			return nil, err
		}
	}
	if lang == "" {
		lang = s.Language(ctx, userID, 0)
	}
	err = sendMail(ctx, cfg, recipients, templates.NoticeText(templates.NoticeMailTestSubject, lang),
		templates.NoticeText(templates.NoticeMailTestBody, lang)+"\n")
	return recipients, err
}

// notifyDelivery mails the leads a round has just delivered and records the outcome on
// the round. It never fails the round: a send error is stored and returned as a status.
func (s *Service) notifyDelivery(ctx context.Context, scope Scope, run database.GrowthRun, leads []LeadView, target int, deadline time.Time) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*smtpTimeout)
	defer cancel()

	status, reason := NotifySent, ""
	cfg, err := s.ConfigFor(ctx, scope.UserID)
	switch {
	case err != nil:
		status, reason = NotifyFailed, err.Error()
	case cfg.SMTPHost == "":
		status, reason = NotifySkipped, ErrNoSMTP.Error()
	default:
		if _, err = s.mailLeads(ctx, cfg, scope, run.ID, leads, target, deadline); err != nil {
			status, reason = NotifyFailed, truncateRunes(err.Error(), 500)
		}
	}

	if err := s.db.RecordGrowthRunNotify(ctx, database.RecordGrowthRunNotifyParams{
		ID: run.ID, NotifyStatus: status, NotifyError: reason,
	}); err != nil {
		reason = strings.TrimSpace(reason + "; failed to record: " + err.Error())
	}
	if reason != "" {
		return status + ": " + reason
	}
	return status
}

// Language is the language the customer's mails and files are written in: the flow's,
// else that of their newest flow; empty, which reads as English, without one.
func (s *Service) Language(ctx context.Context, userID, flowID int64) string {
	if flowID != 0 {
		if flow, err := s.db.GetFlow(ctx, flowID); err == nil && flow.UserID == userID {
			return flow.Language
		}
	}
	flows, err := s.db.GetUserFlows(ctx, userID)
	if err != nil || len(flows) == 0 {
		return ""
	}
	return flows[0].Language
}

func (s *Service) flowTitle(ctx context.Context, scope Scope) string {
	if scope.FlowID == 0 {
		return ""
	}
	flow, err := s.db.GetFlow(ctx, scope.FlowID)
	if err != nil || flow.UserID != scope.UserID {
		return ""
	}
	return flow.Title
}

// deliverySummary: a non-zero deadline is the schedule's end time the round was closed at.
func deliverySummary(business string, leads []LeadView, target int, deadline time.Time, lang string) (string, string) {
	text := func(n templates.Notice) string { return templates.NoticeText(n, lang) }
	if business == "" {
		business = text(templates.NoticeMailDefaultBusiness)
	}
	subject := fmt.Sprintf(text(templates.NoticeMailSubject), business, len(leads))

	scores := make(map[int]int)
	for _, l := range leads {
		scores[l.Score]++
	}
	keys := make([]int, 0, len(scores))
	for k := range scores {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(keys)))

	var b strings.Builder
	fmt.Fprintf(&b, text(templates.NoticeMailBusiness)+"\n", business)
	fmt.Fprintf(&b, text(templates.NoticeMailDelivered)+"\n", len(leads), target)
	if !deadline.IsZero() {
		fmt.Fprintf(&b, text(templates.NoticeMailAtDeadline)+"\n", deadline.UTC().Format("2006-01-02 15:04"), len(leads), target)
	}
	b.WriteString("\n" + text(templates.NoticeMailScoreHeader) + "\n")
	for _, k := range keys {
		fmt.Fprintf(&b, text(templates.NoticeMailScoreLine)+"\n", k, scores[k])
	}
	b.WriteString("\n" + text(templates.NoticeMailAttached) + "\n")
	return subject, b.String()
}

func (s *Service) mailLeads(
	ctx context.Context,
	cfg *config.Config,
	scope Scope,
	runID int64,
	leads []LeadView,
	target int,
	deadline time.Time,
) ([]string, error) {
	to, err := s.notifyRecipients(ctx, scope.UserID)
	if err != nil {
		return nil, err
	}
	sheet := s.LeadsCSVContext(ctx, scope.UserID, scope.FlowID)
	subject, body := deliverySummary(s.flowTitle(ctx, scope), leads, target, deadline, sheet.Lang)
	return to, sendMail(ctx, cfg, to, subject, body, attachment{
		name:        LeadsCSVFileName(s.businessName(ctx, scope), s.sheetScope(ctx, scope, runID, sheet), sheet, time.Now()),
		contentType: "text/csv; charset=utf-8",
		data:        LeadsCSV(leads, sheet),
	})
}

func (s *Service) businessName(ctx context.Context, scope Scope) string {
	profile, err := s.ProfileForFlow(ctx, scope.UserID, scope.FlowID)
	if err != nil {
		return ""
	}
	return profile.Name
}

// sheetScope names what the attached sheet holds the way the web app names its download:
// the round's number for one round, else the flow's delivered leads.
func (s *Service) sheetScope(ctx context.Context, scope Scope, runID int64, sheet LeadsCSVContext) string {
	if runID == 0 {
		return sheet.DeliveredScope()
	}
	var flowID *int64
	if scope.FlowID != 0 {
		flowID = &scope.FlowID
	}
	runs, err := s.ListRuns(ctx, scope.UserID, flowID)
	if err != nil {
		return sheet.DeliveredScope()
	}
	if round, ok := RoundNumbers(runs)[runID]; ok {
		return sheet.RoundScope(round)
	}
	return sheet.DeliveredScope()
}

// EmailLeads mails the leads of one round on demand, as the email of a finished round:
// the round's delivered leads, or its qualified leads while it has delivered none.
// Without a task it mails the flow's delivered leads, else its qualified ones.
func (s *Service) EmailLeads(ctx context.Context, userID, flowID int64, taskID *int64) ([]string, error) {
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	if cfg.SMTPHost == "" {
		return nil, ErrNoSMTP
	}
	scope := Scope{UserID: userID, FlowID: flowID, TaskID: taskID}

	var (
		runID  int64
		target int
		leads  []LeadView
	)
	if taskID != nil {
		run, err := s.db.GetGrowthRunByTask(ctx, sql.NullInt64{Int64: *taskID, Valid: true})
		switch {
		case err == nil && run.UserID == userID:
			runID, target = run.ID, int(run.Target)
			if leads, err = s.ListLeads(ctx, userID, LeadFilter{RunID: &run.ID, Limit: maxListLimit}); err != nil {
				return nil, err
			}
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return nil, fmt.Errorf("failed to load the round of task %d: %w", *taskID, err)
		}
	}
	if len(leads) == 0 {
		delivered := true
		f := LeadFilter{FlowID: &flowID, TaskID: taskID, Delivered: &delivered, Limit: maxListLimit}
		if taskID == nil {
			if leads, err = s.ListLeads(ctx, userID, f); err != nil {
				return nil, err
			}
		}
		if len(leads) == 0 {
			f.Delivered, f.Tier = nil, TierQualified
			if leads, err = s.ListLeads(ctx, userID, f); err != nil {
				return nil, err
			}
		}
	}
	if len(leads) == 0 {
		return nil, ErrNoLeadsToSend
	}
	if target == 0 {
		target = len(leads)
	}
	return s.mailLeads(ctx, cfg, scope, runID, leads, target, time.Time{})
}
