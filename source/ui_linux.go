//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: converter file.xlsx [file2.xlsx ...]")
		return
	}
	for _, f := range os.Args[1:] {
		o := convertWorkbook(f)
		fmt.Printf("%s: output=%s images=%d unmatched=%d err=%v\n", f, o.output, o.converted, o.unmatched, o.err)
	}
}

