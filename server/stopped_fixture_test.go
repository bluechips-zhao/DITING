package server

import "context"

// Metadata/configuration fixtures do not run the engine or its background jobs.
func newStoppedTestServer(m *Manager, skills, data, keys string) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(ctx, m, skills, data, keys)
	s.archiveWG.Wait()
	<-s.side.done
	return s
}
