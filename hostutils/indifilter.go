package hostutils

// INDIFilterConfig contains config for incoming and outgoinf traffic rules
type INDIFilterConfig struct {
	IncomingRules map[string]interface{} // TODO: design format for rules
	OutgoingRules map[string]interface{} // TODO: design format for rules
}

// INDIFilter provides logic for incoming/outgoing traffic
type INDIFilter struct {
	config *INDIFilterConfig
}

func NewINDIFilter(config *INDIFilterConfig) *INDIFilter {
	return &INDIFilter{
		config: config,
	}
}

// FilterOutgoing filters out outgoing traffic
func (f *INDIFilter) FilterOutgoing(data [][]byte) [][]byte {
	// TODO: apply f.OutgoingRules
	return data
}

// FilterIncoming filters out outgoing traffic
func (f *INDIFilter) FilterIncoming(data [][]byte) [][]byte {
	// TODO: apply f.IncomingRules
	return data
}
