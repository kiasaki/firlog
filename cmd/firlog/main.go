package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/kiasaki/firlog"
)

func main() {
	var port string
	flag.StringVar(&port, "port", getEnv("PORT", "3000"), "Port for the HTTP server to listen on")

	var dataDir string
	flag.StringVar(&dataDir, "dataDir", "data", "Specifies the directory to store data in")

	var tokensString string
	flag.StringVar(&tokensString, "tokens", getEnv("TOKENS", ""), "Valid auth tokens")
	if len(tokensString) == 0 {
		log.Fatalln("Missing `tokens` config")
	}
	tokens := strings.Split(tokensString, ",")

	var basicAuthString string
	flag.StringVar(&basicAuthString, "basic-auth", getEnv("BASIC_AUTH", ""), "'user:pass' pair for basic auth")
	basicAuthCredentials := strings.SplitN(basicAuthString, ":", 2)
	if len(basicAuthCredentials) != 2 {
		log.Fatalln("Missing `basic-auth` config")
	}

	app := firlog.NewApp(dataDir, tokens)
	app.Start(port, basicAuthCredentials[0], basicAuthCredentials[1])
}

func getEnv(name, alt string) string {
	value := os.Getenv(name)
	if value == "" {
		return alt
	}
	return value
}
