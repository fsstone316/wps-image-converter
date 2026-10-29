package main

import (
	"reflect"
	"testing"
	"unicode/utf16"
)

func TestWin32FileFilterEncoding(t *testing.T) {
	parts := []string{"Excel 文件 (*.xlsx;*.xlsm)", "*.xlsx;*.xlsm", "所有文件 (*.*)", "*.*"}
	got := multiStringUTF16(parts...)
	if got[len(got)-1] != 0 || got[len(got)-2] != 0 {
		t.Fatalf("missing final double NUL: %v", got[len(got)-3:])
	}
	var parsed []string
	var part []uint16
	for _, c := range got {
		if c == 0 {
			if len(part) == 0 {
				break
			}
			parsed = append(parsed, string(utf16.Decode(part)))
			part = nil
		} else {
			part = append(part, c)
		}
	}
	if !reflect.DeepEqual(parsed, parts) {
		t.Fatalf("roundtrip mismatch: %#v", parsed)
	}
}

