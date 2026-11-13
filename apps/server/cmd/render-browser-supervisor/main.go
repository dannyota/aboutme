package main

import (
	"os"

	"danny.vn/aboutme/apps/server/internal/renderprocess"
)

func main() {
	os.Exit(renderprocess.Run(os.Args[1:]))
}
