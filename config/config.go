package config

import (
	"os"
	"strconv"
)

type Config struct {
	Debug          bool
	ListenAddress  string
	Key            string
	IPV6           bool
	RequestTimeout uint8
	FollowRedirect bool
}

var DefaultConfig *Config

func init() {
	var requestTimeout uint8 = 5
	requestTimeoutStr := os.Getenv("MORTY_REQUEST_TIMEOUT")

	if requestTimeoutStr != "" {
		parsedUint, err := strconv.ParseUint(requestTimeoutStr, 10, 8)
		if err == nil {
			requestTimeout = uint8(parsedUint)
		}
	}

	DefaultConfig = &Config{
		Debug:          os.Getenv("DEBUG") == "true",
		ListenAddress:  os.Getenv("MORTY_ADDRESS"),
		Key:            "",
		IPV6:           os.Getenv("MORTY_IPV6") == "true",
		RequestTimeout: requestTimeout,
		FollowRedirect: os.Getenv("MORTY_FOLLOW_REDIRECTS") == "true",
	}
}
