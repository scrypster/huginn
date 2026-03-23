package relay

// InProcessHub is the default Hub. It is a no-op.
type InProcessHub struct{}

func NewInProcessHub() *InProcessHub {
	return &InProcessHub{}
}

func (h *InProcessHub) Send(_ string, _ Message) error {
	return nil
}

func (h *InProcessHub) Close(_ string) {
}
