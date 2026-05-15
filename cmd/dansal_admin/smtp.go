package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"text/tabwriter"
	"os"
)

func cmdSMTPShow(_ []string) {
	resp := send(socketPath, request{Cmd: "smtp-get"})
	if !resp.OK {
		die("%s", resp.Error)
	}
	var cfg map[string]interface{}
	json.Unmarshal(resp.Data, &cfg)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, k := range []string{"host", "port", "username", "from", "from_name", "tls", "password_set"} {
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
	fs.Parse(args)

	if *host == "" && *port == 0 && *username == "" && *from == "" && *fromName == "" && *tlsMode == "" {
		die("at least one flag is required (see smtp-set --help)")
	}

	resp := send(socketPath, request{
		Cmd:          "smtp-set",
		SMTPHost:     *host,
		SMTPPort:     *port,
		SMTPUsername: *username,
		SMTPFrom:     *from,
		SMTPFromName: *fromName,
		SMTPTLS:      *tlsMode,
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

	resp := send(socketPath, request{Cmd: "smtp-test", SMTPTo: *to})
	if !resp.OK {
		die("%s", resp.Error)
	}
	fmt.Printf("test email sent to %s\n", *to)
}
