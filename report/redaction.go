package report

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/Autumn-27/artex/db"
)

const Redacted = "[REDACTED]"

var credentialToken = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]{20,}|gh[pousr]_[a-z0-9]{20,}|github_pat_[a-z0-9_]{20,}|eyJ[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+)\b`)
var credentialBearer = regexp.MustCompile(`(?i)\b(?:bearer|basic)[ \t]+[a-z0-9._~+/-]+=*`)
var credentialHeader = regexp.MustCompile(`(?im)((?:authorization|proxy-authorization|cookie|set-cookie|x-api-key|x-auth-token)[ \t]*:)[^\r\n]*`)
var credentialField = regexp.MustCompile(`(?i)((?:"|')?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|token|password|passwd|pwd|client[_-]?secret|secret|session[_-]?id|密码|密钥)(?:"|')?[ \t]*[:=][ \t]*)("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;&}]+)`)
var credentialURL = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]{0,20}://[^\s<>"'` + "`" + `]+`)

func sensitiveField(name string) bool {
	name = strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(name))
	switch name {
	case "key", "apikey", "token", "accesstoken", "refreshtoken", "idtoken", "password", "passwd", "pwd", "secret", "clientsecret", "authorization", "proxyauthorization", "cookie", "setcookie", "xapikey", "xauthtoken", "session", "sessionid", "密码", "密钥":
		return true
	}
	return false
}

// RedactText masks recognized credential forms, not arbitrary unlabeled secrets.
// Original records are never modified; raw traffic bodies are omitted separately.
func RedactText(text string) string {
	text = credentialToken.ReplaceAllString(text, Redacted)
	text = credentialBearer.ReplaceAllString(text, Redacted)
	text = credentialHeader.ReplaceAllString(text, "${1} "+Redacted)
	text = credentialField.ReplaceAllString(text, "${1}"+Redacted)
	return credentialURL.ReplaceAllStringFunc(text, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return Redacted
		}
		if u.User != nil {
			u.User = url.User("REDACTED")
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			u.RawQuery = "redacted=unparsed"
		} else {
			for key := range query {
				if sensitiveField(key) {
					query.Set(key, Redacted)
				}
			}
			u.RawQuery = query.Encode()
		}
		return u.String()
	})
}

// RedactJSON keeps JSON valid and preserves number precision and object shape.
func RedactJSON(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		return nil, err
	}
	var walk func(any) any
	walk = func(v any) any {
		switch v := v.(type) {
		case map[string]any:
			for key, child := range v {
				if sensitiveField(key) {
					v[key] = Redacted
				} else {
					v[key] = walk(child)
				}
			}
		case []any:
			for i := range v {
				v[i] = walk(v[i])
			}
		case string:
			return RedactText(v)
		}
		return v
	}
	return walk(parsed), nil
}

func RedactedFindings(findings []*db.DBFinding) []*db.DBFinding {
	out := make([]*db.DBFinding, 0, len(findings))
	for _, original := range findings {
		f := *original
		for _, text := range []*string{&f.Name, &f.VulnClass, &f.Summary, &f.Evidence, &f.Report, &f.TaskDescription, &f.DuplicateReason, &f.Worker} {
			*text = RedactText(*text)
		}
		f.AssetIDs = append([]int64(nil), original.AssetIDs...)
		f.TrafficBindings = append([]db.FindingTrafficBinding(nil), original.TrafficBindings...)
		for i := range f.TrafficBindings {
			binding := &f.TrafficBindings[i]
			binding.Note = RedactText(binding.Note)
			binding.Snapshot.URL = RedactText(binding.Snapshot.URL)
			binding.Snapshot.ContentType = RedactText(binding.Snapshot.ContentType)
			binding.Snapshot.ReqHead, binding.Snapshot.RespHead = "", ""
		}
		out = append(out, &f)
	}
	return out
}
