package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Autumn-27/artex/db"
	"net/http"
	"os"
	"strings"
)

func (s *Server) searchConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if len(q.Get("q")) > 1000 {
		writeErr(w, 400, "query too long")
		return
	}
	items, next, total, err := s.m.pg.SearchConversations(r.Context(), q.Get("q"), q.Get("agent"), q.Get("cursor"), atoiDefault(q.Get("limit"), 50))
	if err != nil {
		status := 500
		if errors.Is(err, db.ErrConversationCursor) {
			status = 400
		}
		writeErr(w, status, err.Error())
		return
	}
	out := make([]conversationListItem, 0, len(items))
	s.chatMu.Lock()
	for _, c := range items {
		out = append(out, conversationListItem{Conversation: c, Running: s.chatBusy[s.convBusyKey(c.ID)]})
	}
	s.chatMu.Unlock()
	writeJSON(w, 200, map[string]any{"conversations": out, "next_cursor": next, "total": total})
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.convByID(w, r)
	if !ok {
		return
	}
	s.chatMu.Lock()
	running := s.chatBusy[s.convBusyKey(c.ID)]
	s.chatMu.Unlock()
	writeJSON(w, 200, conversationListItem{Conversation: c, Running: running})
}

func (s *Server) exportConversation(w http.ResponseWriter, r *http.Request) {
	pg, c, ok := s.convByID(w, r)
	if !ok {
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "md"
	}
	if format != "md" && format != "json" {
		writeErr(w, 400, "format must be md or json")
		return
	}
	f, err := os.CreateTemp("", "diting-conversation-export-*")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	var writeErrValue error
	write := func(text string) {
		if writeErrValue == nil {
			_, writeErrValue = f.WriteString(text)
		}
	}
	if format == "md" {
		write(fmt.Sprintf("# %s\n\nAgent: %s · 会话 #%d\n\n附件以原始引用保留。\n\n", c.Title, c.AgentKey, c.ID))
	} else {
		raw, _ := json.Marshal(c)
		write(`{"conversation":` + string(raw) + `,"activities":[`)
	}
	first := true
	err = pg.VisitConversationExport(r.Context(), c.ID, r.URL.Query().Get("thinking") == "true", r.URL.Query().Get("tools") != "false", func(a db.Activity) error {
		if format == "json" {
			if !first {
				write(",")
			}
			raw, e := json.Marshal(struct {
				db.Activity
				Detail string `json:"detail"`
			}{a, a.Detail})
			if e != nil {
				return e
			}
			write(string(raw))
			first = false
		} else {
			body := a.Detail
			if strings.TrimSpace(body) == "" {
				body = a.Summary
			}
			write(fmt.Sprintf("## %s · %s %s\n\n%s\n\n", a.CreatedAt.Format("2006-01-02 15:04:05"), a.Kind, a.Tool, body))
		}
		return writeErrValue
	})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if format == "json" {
		write("]}\n")
	}
	if writeErrValue != nil {
		writeErr(w, 500, writeErrValue.Error())
		return
	}
	if _, err = f.Seek(0, 0); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	info, err := f.Stat()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", map[string]string{"md": "text/markdown; charset=utf-8", "json": "application/json"}[format])
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="conversation-%d.%s"`, c.ID, format))
	http.ServeContent(w, r, "conversation."+format, info.ModTime(), f)
}
