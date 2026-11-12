package parser

import (
	"fmt"
	"testing"
)

func TestParseAttributesRaw(t *testing.T) {
	source := []byte("{=html}")
	events, nextPos, ok := ParseAttributes(source, 0, int32(len(source)))

	fmt.Printf("ok: %v, nextPos: %d\n", ok, nextPos)
	for _, ev := range events {
		fmt.Printf("Event: type=%d, start=%d, end=%d\n", ev.Type, ev.Start, ev.End)
		if ev.Type == 106 {
			fmt.Printf("  Key content: %q\n", source[ev.Start:ev.End+1])
		}
	}
}
