package state

import (
	"strings"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

func (p Policy) ValidateCursor(context CursorContext) Decision {
	limit := int64(p.captureLimit)
	if context.Captured < 0 || context.Captured > limit {
		return corruptStateDecision()
	}
	if context.Cursor < 0 || context.Cursor > context.Captured {
		return Decision{Allowed: false, Code: p.cursor.invalidCursorCode}
	}
	return Decision{Allowed: true, Code: CodeAllow}
}

func (p Policy) ValidateRange(context RangeContext) Decision {
	limit := int64(p.captureLimit)
	if context.Captured < 0 || context.Captured > limit {
		return corruptStateDecision()
	}
	if context.Start < 0 || context.End < context.Start || context.End > context.Captured {
		return Decision{Allowed: false, Code: p.cursor.invalidRangeCode}
	}
	return Decision{Allowed: true, Code: CodeAllow}
}

func (p Policy) ResolveOutputRange(payload generated.OutputPayload, captured int64) (RangeContext, Decision) {
	start := int64(0)
	if payload.StartCursorBytes != nil {
		start = int64(*payload.StartCursorBytes)
	}
	end := captured
	if payload.EndCursorBytes != nil {
		end = int64(*payload.EndCursorBytes)
	}
	resolved := RangeContext{Start: start, End: end, Captured: captured}
	return resolved, p.ValidateRange(resolved)
}

func (p Policy) Output(raw []byte, context OutputContext) (generated.OutputChunk, Decision) {
	decision := p.ValidateRange(context.Range)
	if !decision.Allowed {
		return generated.OutputChunk{}, decision
	}
	if context.Range.Captured != int64(len(raw)) {
		return generated.OutputChunk{}, Decision{Allowed: false, Code: CodeCorruptState}
	}
	text := strings.ToValidUTF8(string(raw[context.Range.Start:context.Range.End]), "\uFFFD")
	return generated.OutputChunk{
		CapturedBytes:    generated.ByteCursor(context.Range.Captured),
		Eof:              context.Terminal && context.Range.End == context.Range.Captured,
		NextCursorBytes:  generated.ByteCursor(context.Range.End),
		StartCursorBytes: generated.ByteCursor(context.Range.Start),
		Text:             text,
	}, decision
}
