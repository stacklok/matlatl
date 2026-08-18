package main

import (
	"os"

	"github.com/stacklok/matlatl/eval/internal/fakeopencode"
)

func main() { os.Exit(fakeopencode.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
