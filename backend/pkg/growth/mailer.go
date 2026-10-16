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

var ErrNoSMTP = errors.New("no SMTP server is configured")

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
// to overrides the recipients when non-empty.
func (s *Service) SendTestEmail(ctx context.Context, userID int64, to string) ([]string, error) {
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
	err = sendMail(ctx, cfg, recipients, "Lead delivery email test",
		"This is a test message. Lead lists will be delivered to this address when a round finishes.\n")
	return recipients, err
}

// notifyDelivery mails the leads a round has just delivered and records the outcome on
// the round. It never fails the round: a send error is stored and returned as a status.
func (s *Service) notifyDelivery(ctx context.Context, scope Scope, run database.GrowthRun, leads []LeadView, target int) string {
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
		var to []string
		if to, err = s.notifyRecipients(ctx, scope.UserID); err == nil {
			subject, body := deliverySummary(s.flowTitle(ctx, scope), leads, target)
			err = sendMail(ctx, cfg, to, subject, body, attachment{
				name:        fmt.Sprintf("leads-round-%d.csv", run.ID),
				contentType: "text/csv; charset=utf-8",
				data:        LeadsCSV(leads),
			})
		}
		if err != nil {
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

func (s *Service) flowTitle(ctx context.Context, scope Scope) string {
	if scope.FlowID == 0 {
		return ""
	}
	flow, err := s.db.GetFlow(ctx, scope.FlowID)
	if err != nil {
		return ""
	}
	return flow.Title
}

func deliverySummary(business string, leads []LeadView, target int) (string, string) {
	if business == "" {
		business = "Lead search"
	}
	subject := fmt.Sprintf("%s: %d leads delivered", business, len(leads))

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
	fmt.Fprintf(&b, "Business: %s\n", business)
	fmt.Fprintf(&b, "Leads delivered: %d (target %d)\n\n", len(leads), target)
	b.WriteString("Score distribution:\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "  score %d: %d\n", k, scores[k])
	}
	b.WriteString("\nThe full list with evidence is attached as CSV.\n")
	return subject, b.String()
}
