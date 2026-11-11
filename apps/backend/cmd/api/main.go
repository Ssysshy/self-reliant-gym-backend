package main

import (
	"gym/internal/config"
	"gym/internal/handler"
)

func main() {
	config.LoadDotEnv()
	handler.Run()
}
