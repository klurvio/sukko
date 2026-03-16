package server

// writePump delegates to the Pump's WriteLoop for testability.
// This is a thin wrapper that connects the Server's context to the Pump.
func (s *Server) writePump(c *Client) {
	defer s.wg.Done()
	s.pump.WriteLoop(s.ctx, c)
}
