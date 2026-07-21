package state

func (p Policy) AcceptedBytes(captured ByteCount, incoming ByteCount, effectiveLimit ByteCount) (ByteCount, Decision) {
	if effectiveLimit == 0 || effectiveLimit > p.captureLimit {
		return 0, Decision{Allowed: false, Code: CodeInvalidRange}
	}
	if captured > effectiveLimit {
		return 0, Decision{Allowed: false, Code: CodeCorruptState}
	}
	if captured == effectiveLimit {
		return 0, Decision{Allowed: true, Code: CodeAllow}
	}
	remaining := effectiveLimit - captured
	if incoming < remaining {
		return incoming, Decision{Allowed: true, Code: CodeAllow}
	}
	return remaining, Decision{Allowed: true, Code: CodeAllow}
}

func (p Policy) AcceptedIncomingPrefix(captured ByteCount, incoming []byte, effectiveLimit ByteCount) ([]byte, Decision) {
	accepted, decision := p.AcceptedBytes(captured, ByteCount(len(incoming)), effectiveLimit)
	if !decision.Allowed {
		return nil, decision
	}
	result := make([]byte, accepted)
	copy(result, incoming[:accepted])
	return result, decision
}
