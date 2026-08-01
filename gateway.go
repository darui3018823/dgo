package dgo

// GatewayCloseRecovery describes how dgo will handle a Gateway connection
// close.
type GatewayCloseRecovery uint8

const (
	// GatewayCloseRecoveryStop means the close is terminal and automatic
	// reconnection has stopped.
	GatewayCloseRecoveryStop GatewayCloseRecovery = iota
	// GatewayCloseRecoveryResume means dgo will reconnect and resume the
	// existing session.
	GatewayCloseRecoveryResume
	// GatewayCloseRecoveryIdentify means dgo will discard the old session and
	// create a new one.
	GatewayCloseRecoveryIdentify
)
