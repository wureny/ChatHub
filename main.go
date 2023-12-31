package main

import (
	"flag"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

func main() {
	flag.Parse()
}
