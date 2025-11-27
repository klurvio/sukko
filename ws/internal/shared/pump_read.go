package shared

// readPump delegates to the Pump's ReadLoop for testability.
// This is a thin wrapper that connects the Server's dependencies to the Pump.
func (s *Server) readPump(c *Client) {
	s.pump.ReadLoop(s.ctx, c, s.disconnectClient, s.handleClientMessage)
}
