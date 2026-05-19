package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
)

func cmdSMTPShow(_ []string) {
	resp := send(socketPath, request{Cmd: "smtp-get"})
	if !resp.OK {
		die("%s", resp.Error)
	}
	var cfg map[string]any
	json.Unmarshal(resp.Data, &cfg)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, k := range []string{"host", "port", "username", "from", "from_name", "tls", "timeout_secs", "password_set"} {
		fmt.Fprintf(tw, "%s\t%v\n", k, cfg[k])
	}
	tw.Flush()
}

func cmdSMTPSet(args []string) {
	fs := flag.NewFlagSet("smtp-set", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["smtp-set"]) }
	host := fs.String("host", "", "SMTP server hostname")
	port := fs.Int("port", 0, "SMTP server port")
	username := fs.String("username", "", "SMTP account username")
	from := fs.String("from", "", "envelope From address")
	fromName := fs.String("from-name", "", "display name in From header")
	tlsMode := fs.String("tls", "", "TLS mode: starttls, tls, none")
	timeout := fs.Int("timeout", 0, "dial and send timeout in seconds (default 30)")
	fs.Parse(args)

	if *host == "" && *port == 0 && *username == "" && *from == "" && *fromName == "" && *tlsMode == "" && *timeout == 0 {
		die("at least one flag is required (see smtp-set --help)")
	}

	resp := send(socketPath, request{
		Cmd:             "smtp-set",
		SMTPHost:        *host,
		SMTPPort:        *port,
		SMTPUsername:    *username,
		SMTPFrom:        *from,
		SMTPFromName:    *fromName,
		SMTPTLS:         *tlsMode,
		SMTPTimeoutSecs: *timeout,
	})
	if !resp.OK {
		die("%s", resp.Error)
	}
	fmt.Println("SMTP settings updated.")
	cmdSMTPShow(nil)
}

func cmdSMTPSetPassword(args []string) {
	fs := flag.NewFlagSet("smtp-set-password", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["smtp-set-password"]) }
	password := fs.String("password", "", "SMTP account password (prompted if omitted)")
	fs.Parse(args)

	pw := *password
	if pw == "" {
		b, err := promptPassword("SMTP password: ")
		if err != nil {
			die("prompt: %v", err)
		}
		pw = string(b)
	}

	resp := send(socketPath, request{Cmd: "smtp-set-password", Password: pw})
	if !resp.OK {
		die("%s", resp.Error)
	}
	fmt.Println("SMTP password updated.")
}

func cmdSMTPTest(args []string) {
	fs := flag.NewFlagSet("smtp-test", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["smtp-test"]) }
	to := fs.String("to", "", "recipient email address")
	fs.Parse(args)

	if *to == "" {
		die("--to is required")
	}

	// Fetch and display current config so the user knows what is being tested.
	cfgResp := send(socketPath, request{Cmd: "smtp-get"})
	if !cfgResp.OK {
		die("could not read SMTP config: %s", cfgResp.Error)
	}
	var cfg map[string]any
	json.Unmarshal(cfgResp.Data, &cfg)

	port := cfg["port"]
	if port == nil || port == float64(0) {
		port = 587
	}
	tls := cfg["tls"]
	if tls == nil || tls == "" {
		tls = "starttls"
	}
	timeout := cfg["timeout_secs"]
	if timeout == nil || timeout == float64(0) {
		timeout = 30
	}

	fmt.Println("SMTP configuration:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  host\t%v:%v\n", cfg["host"], port)
	fmt.Fprintf(tw, "  tls\t%v\n", tls)
	fmt.Fprintf(tw, "  username\t%v\n", cfg["username"])
	fmt.Fprintf(tw, "  from\t%v\n", cfg["from"])
	fmt.Fprintf(tw, "  timeout\t%vs\n", timeout)
	fmt.Fprintf(tw, "  password_set\t%v\n", cfg["password_set"])
	tw.Flush()
	fmt.Printf("\nSending test email to %s...\n", *to)

	resp := send(socketPath, request{Cmd: "smtp-test", SMTPTo: *to})
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "error: %s\n", resp.Error)
		os.Exit(1)
	}
	fmt.Println("ok")
}
