package connectors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/gotd/td/tg"
)

const (
	refChannel = "channel"
	refChat    = "chat"
	refInvite  = "invite"
	refUser    = "user"
)

// tgRef addresses a chat or user for one account; as a source it is stored in
// Source.Peer. A channel reached through an invite link keeps the invite hash:
// a private channel can only be joined through it, while reading needs the id
// and access hash.
type tgRef struct {
	Kind     string `json:"k"`
	ID       int64  `json:"id,omitempty"`
	Hash     int64  `json:"h,omitempty"`
	Invite   string `json:"i,omitempty"`
	Username string `json:"u,omitempty"`
	// A "min" user seen in a message has no usable access hash; the API
	// addresses it through the message it was seen in. Users are never stored
	// as peers, so these stay in memory.
	FromPeer *tgRef `json:"-"`
	FromMsg  int    `json:"-"`
}

func (r tgRef) peer() json.RawMessage {
	data, _ := json.Marshal(r)
	return data
}

func peerRef(raw json.RawMessage) (tgRef, error) {
	var r tgRef
	if err := json.Unmarshal(raw, &r); err != nil {
		return tgRef{}, fmt.Errorf("malformed telegram peer: %w", err)
	}
	switch r.Kind {
	case refChannel, refChat:
		if r.ID == 0 {
			return tgRef{}, fmt.Errorf("telegram peer of kind %q has no id", r.Kind)
		}
	case refInvite:
		if r.Invite == "" {
			return tgRef{}, errors.New("telegram invite peer has no hash")
		}
	default:
		return tgRef{}, fmt.Errorf("telegram peer of kind %q is not a source", r.Kind)
	}
	return r, nil
}

func (r tgRef) inputPeer() (tg.InputPeerClass, bool) {
	switch r.Kind {
	case refChannel:
		return &tg.InputPeerChannel{ChannelID: r.ID, AccessHash: r.Hash}, true
	case refChat:
		return &tg.InputPeerChat{ChatID: r.ID}, true
	case refUser:
		return &tg.InputPeerUser{UserID: r.ID, AccessHash: r.Hash}, true
	}
	return nil, false
}

func (r tgRef) inputChannel() (tg.InputChannelClass, bool) {
	if r.Kind != refChannel {
		return nil, false
	}
	return &tg.InputChannel{ChannelID: r.ID, AccessHash: r.Hash}, true
}

func (r tgRef) inputUser() (tg.InputUserClass, bool) {
	if r.Kind != refUser {
		return nil, false
	}
	if r.FromPeer != nil {
		peer, ok := r.FromPeer.inputPeer()
		if !ok {
			return nil, false
		}
		return &tg.InputUserFromMessage{Peer: peer, MsgID: r.FromMsg, UserID: r.ID}, true
	}
	return &tg.InputUser{UserID: r.ID, AccessHash: r.Hash}, true
}

type tgLink struct {
	Username string
	Invite   string
}

var (
	usernameRe   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)
	inviteHashRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,}$`)
	linkHosts    = map[string]bool{"t.me": true, "telegram.me": true, "telegram.dog": true}
	reservedPath = map[string]bool{
		"addstickers": true, "addemoji": true, "addlist": true, "addtheme": true,
		"share": true, "proxy": true, "socks": true, "iv": true, "login": true,
		"setlanguage": true, "confirmphone": true, "boost": true, "giftcode": true,
		"invoice": true, "m": true, "joinchat": true, "c": true, "s": true,
	}
)

func parseTelegramLink(raw string) (tgLink, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return tgLink{}, errors.New("empty link")
	}
	if strings.HasPrefix(s, "@") {
		return usernameLink(s[1:])
	}
	if usernameRe.MatchString(s) {
		return tgLink{Username: s}, nil
	}

	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "tg://") {
		u, err := url.Parse(s)
		if err != nil {
			return tgLink{}, fmt.Errorf("malformed link %q", raw)
		}
		switch strings.ToLower(u.Host) {
		case "resolve":
			return usernameLink(u.Query().Get("domain"))
		case "join":
			return inviteLink(u.Query().Get("invite"))
		}
		return tgLink{}, fmt.Errorf("unsupported link %q", raw)
	}
	if !strings.Contains(lower, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return tgLink{}, fmt.Errorf("malformed link %q", raw)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if !linkHosts[host] {
		// username.t.me
		for base := range linkHosts {
			if sub, ok := strings.CutSuffix(host, "."+base); ok && !strings.Contains(sub, ".") {
				return usernameLink(sub)
			}
		}
		return tgLink{}, fmt.Errorf("not a Telegram link: %q", raw)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	first := parts[0]
	switch {
	case first == "":
		return tgLink{}, fmt.Errorf("link %q names no chat", raw)
	case strings.HasPrefix(first, "+"):
		return inviteLink(first[1:])
	case strings.EqualFold(first, "joinchat") && len(parts) > 1:
		return inviteLink(parts[1])
	case first == "s" && len(parts) > 1:
		return usernameLink(parts[1])
	case first == "c":
		return tgLink{}, fmt.Errorf("%q links into a private chat by id; use its invite link instead", raw)
	case reservedPath[strings.ToLower(first)]:
		return tgLink{}, fmt.Errorf("%q is not a chat link", raw)
	}
	return usernameLink(first)
}

func usernameLink(name string) (tgLink, error) {
	if !usernameRe.MatchString(name) {
		return tgLink{}, fmt.Errorf("invalid username %q", name)
	}
	return tgLink{Username: name}, nil
}

func inviteLink(hash string) (tgLink, error) {
	if !inviteHashRe.MatchString(hash) {
		return tgLink{}, fmt.Errorf("invalid invite hash %q", hash)
	}
	return tgLink{Invite: hash}, nil
}
