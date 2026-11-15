// Command devicecheckprobe checks that the configured key may call Apple
// DeviceCheck (decision 698). It signs a token with DEVICECHECK_KEY_ID,
// DEVICECHECK_TEAM_ID, and DEVICECHECK_PRIVATE_KEY (each defaulting to its
// APNS_* value), asks query_two_bits about an obviously invalid device
// token, and prints only Apple's HTTP status and short error text, never the
// key or the signed token.
//
//	go run ./cmd/devicecheckprobe              # production server
//	go run ./cmd/devicecheckprobe -development # development server
//
// 400 "Missing or badly formatted device token payload" means the key is
// accepted for DeviceCheck. 401 or 403 means it isn't.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
)

func main() {
	development := flag.Bool("development", false, "ask Apple's development DeviceCheck server")
	flag.Parse()
	os.Exit(run(*development))
}

func run(development bool) int {
	dc, err := config.LoadDeviceCheck(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if !dc.Enabled() {
		fmt.Fprintln(os.Stderr, "no key: set DEVICECHECK_KEY_ID, DEVICECHECK_TEAM_ID, and DEVICECHECK_PRIVATE_KEY, or the APNS_* values")
		return 2
	}
	client := plus.NewDeviceCheckClient(plus.DeviceCheckOptions{KeyID: dc.KeyID, TeamID: dc.TeamID, PrivateKey: dc.PrivateKey})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, body, err := client.Call(ctx, development, "query_two_bits", map[string]any{"device_token": "not-a-real-device-token"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "request failed:", err)
		return 1
	}
	fmt.Printf("HTTP %d: %s\n", status, plus.ShortText(body))
	switch status {
	case 400:
		fmt.Println("The key is accepted for DeviceCheck.")
		return 0
	case 401, 403:
		fmt.Println("The key is not accepted for DeviceCheck.")
		return 1
	}
	return 1
}
