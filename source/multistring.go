package main

import "unicode/utf16"

// multiStringUTF16 creates a Win32 MULTI_SZ buffer.
// syscall.StringToUTF16 must NOT be called on a string containing NULs:
// it panics with "string with NUL passed to StringToUTF16".
// OPENFILENAMEW.lpstrFilter expects NUL-separated fields and a final double NUL.
func multiStringUTF16(parts ...string) []uint16 {
	result := make([]uint16, 0, 256)
	for _, part := range parts {
		result = append(result, utf16.Encode([]rune(part))...)
		result = append(result, 0)
	}
	// The format expects two terminating zero code units.
	result = append(result, 0)
	return result
}

