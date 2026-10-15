package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const loginWindow = 5 * time.Minute
const loginMaxAttempts = 10
const loginMaxPeers = 4096

type loginEntry struct {
	at       time.Time
	attempts int
}
type loginLimiter struct {
	mu    sync.Mutex
	peers map[string]loginEntry
}

// Count attempts before bcrypt to bound both password guessing and CPU usage.
// Forwarded headers are deliberately ignored: an unauthenticated client can forge them.
func (l *loginLimiter) allow(r *http.Request, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.peers == nil {
		l.peers = make(map[string]loginEntry)
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	for key, entry := range l.peers {
		if now.Sub(entry.at) >= loginWindow {
			delete(l.peers, key)
		}
	}
	entry, exists := l.peers[peer]
	if !exists {
		if len(l.peers) >= loginMaxPeers {
			return false, loginWindow
		}
		entry.at = now
	}
	if entry.attempts >= loginMaxAttempts {
		return false, loginWindow - now.Sub(entry.at)
	}
	entry.attempts++
	l.peers[peer] = entry
	return true, 0
}
