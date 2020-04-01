package lib

const (
	GRPCMaxRecvMsgSize = 10 * 1024 * 1024
	GRPCMaxSendMsgSize = 10 * 1024 * 1024

	INDIServerMaxRecvMsgSize = GRPCMaxRecvMsgSize
	INDIServerMaxSendMsgSize = GRPCMaxSendMsgSize

	ModeSolo    = "solo"
	ModeShare   = "share"
	ModeRobotic = "robotic"
)
