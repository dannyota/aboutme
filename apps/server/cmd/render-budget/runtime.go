package main

import "runtime"

func runtimeOS() string           { return runtime.GOOS }
func runtimeArchitecture() string { return runtime.GOARCH }
