package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"text/tabwriter"
)

const defaultSocket = "./dansal.sock"

type request struct {
	Cmd      string `json:"cmd"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
	Password string `json:"password,omitempty"`
	Role     string `json:"role,omitempty"`
	OrgID    int    `json:"org_id,omitempty"`
}

type response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

type user struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

type org struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

type member struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	CreatedAt string `json:"created_at"`
}

func send(socketPath string, req request) response {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot connect to %s: %v\n", socketPath, err)
		os.Exit(1)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return resp
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

var socketPath string

func main() {
	flag.StringVar(&socketPath, "socket", defaultSocket, "path to dansal admin socket")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	sub, rest := args[0], args[1:]

	switch sub {
	case "help":
		cmdHelp(rest)
	case "list-users":
		cmdListUsers(rest)
	case "create-user":
		cmdCreateUser(rest)
	case "delete-user":
		cmdDeleteUser(rest)
	case "set-password":
		cmdSetPassword(rest)
	case "set-role":
		cmdSetRole(rest)
	case "list-orgs":
		cmdListOrgs(rest)
	case "list-members":
		cmdListMembers(rest)
	case "add-member":
		cmdAddMember(rest)
	case "remove-member":
		cmdRemoveMember(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", sub)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: dansal_admin [--socket PATH] <command> [flags]

User management:
  list-users                                         List all users
  create-user  --username STR --email STR            Create a new user
               --password STR [--role STR]
  delete-user  --username STR                        Delete a user
  set-password --username STR --password STR         Change a user's password
  set-role     --username STR --role STR             Change a user's role

Organization management:
  list-orgs                                          List all organizations
  list-members --org-id INT                          List members of an org
  add-member   --org-id INT --username STR           Add user to an org
  remove-member --org-id INT --username STR          Remove user from an org

Roles: admin, user, publisher, viewer

Run 'dansal_admin help <command>' for details on a specific command.`)
}

var commandHelp = map[string]string{
	"list-users": `Usage: dansal_admin list-users

List all users with their ID, username, email, role and creation date.`,

	"create-user": `Usage: dansal_admin create-user --username STR --email STR --password STR [--role STR]

Create a new user account.

Flags:
  --username  Username (required)
  --email     Email address (required)
  --password  Password (required)
  --role      Role: admin, user, publisher, viewer (default: user)`,

	"delete-user": `Usage: dansal_admin delete-user --username STR

Delete a user. Admin accounts cannot be deleted.

Flags:
  --username  Username of the account to delete (required)`,

	"set-password": `Usage: dansal_admin set-password --username STR --password STR

Change the password for any user account.

Flags:
  --username  Username of the target account (required)
  --password  New password (required)`,

	"set-role": `Usage: dansal_admin set-role --username STR --role STR

Change the role of a user account.

Flags:
  --username  Username of the target account (required)
  --role      New role: admin, user, publisher, viewer (required)`,

	"list-orgs": `Usage: dansal_admin list-orgs

List all organizations with their ID, name, description and creation date.`,

	"list-members": `Usage: dansal_admin list-members --org-id INT

List all members of an organization.

Flags:
  --org-id  Organization ID (required)`,

	"add-member": `Usage: dansal_admin add-member --org-id INT --username STR

Add a user to an organization. Has no effect if the user is already a member.

Flags:
  --org-id    Organization ID (required)
  --username  Username to add (required)`,

	"remove-member": `Usage: dansal_admin remove-member --org-id INT --username STR

Remove a user from an organization.

Flags:
  --org-id    Organization ID (required)
  --username  Username to remove (required)`,
}

func cmdHelp(args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(0)
	}
	text, ok := commandHelp[args[0]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		usage()
		os.Exit(1)
	}
	fmt.Println(text)
}

func cmdListUsers(args []string) {
	resp := send(socketPath, request{Cmd: "list-users"})
	if !resp.OK {
		die("%s", resp.Error)
	}
	var users []user
	json.Unmarshal(resp.Data, &users)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUSERNAME\tEMAIL\tROLE\tCREATED")
	for _, u := range users {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", u.ID, u.Username, u.Email, u.Role, u.CreatedAt)
	}
	w.Flush()
}

func cmdCreateUser(args []string) {
	fs := flag.NewFlagSet("create-user", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["create-user"]) }
	username := fs.String("username", "", "username")
	email := fs.String("email", "", "email address")
	password := fs.String("password", "", "password")
	role := fs.String("role", "user", "role (admin|user|publisher|viewer)")
	fs.Parse(args)
	if *username == "" || *email == "" || *password == "" {
		die("--username, --email and --password are required")
	}
	resp := send(socketPath, request{Cmd: "create-user", Username: *username, Email: *email, Password: *password, Role: *role})
	if !resp.OK {
		die("%s", resp.Error)
	}
	var u user
	json.Unmarshal(resp.Data, &u)
	fmt.Printf("created user %s (id=%d, role=%s)\n", u.Username, u.ID, u.Role)
}

func cmdDeleteUser(args []string) {
	fs := flag.NewFlagSet("delete-user", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["delete-user"]) }
	username := fs.String("username", "", "username")
	fs.Parse(args)
	if *username == "" {
		die("--username is required")
	}
	resp := send(socketPath, request{Cmd: "delete-user", Username: *username})
	if !resp.OK {
		die("%s", resp.Error)
	}
	fmt.Printf("deleted user %s\n", *username)
}

func cmdSetPassword(args []string) {
	fs := flag.NewFlagSet("set-password", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["set-password"]) }
	username := fs.String("username", "", "username")
	password := fs.String("password", "", "new password")
	fs.Parse(args)
	if *username == "" || *password == "" {
		die("--username and --password are required")
	}
	resp := send(socketPath, request{Cmd: "set-password", Username: *username, Password: *password})
	if !resp.OK {
		die("%s", resp.Error)
	}
	fmt.Printf("password updated for %s\n", *username)
}

func cmdSetRole(args []string) {
	fs := flag.NewFlagSet("set-role", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["set-role"]) }
	username := fs.String("username", "", "username")
	role := fs.String("role", "", "role (admin|user|publisher|viewer)")
	fs.Parse(args)
	if *username == "" || *role == "" {
		die("--username and --role are required")
	}
	resp := send(socketPath, request{Cmd: "set-role", Username: *username, Role: *role})
	if !resp.OK {
		die("%s", resp.Error)
	}
	fmt.Printf("role updated: %s → %s\n", *username, *role)
}

func cmdListOrgs(args []string) {
	resp := send(socketPath, request{Cmd: "list-orgs"})
	if !resp.OK {
		die("%s", resp.Error)
	}
	var orgs []org
	json.Unmarshal(resp.Data, &orgs)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tDESCRIPTION\tCREATED")
	for _, o := range orgs {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", o.ID, o.Name, o.Description, o.CreatedAt)
	}
	w.Flush()
}

func cmdListMembers(args []string) {
	fs := flag.NewFlagSet("list-members", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["list-members"]) }
	orgID := fs.Int("org-id", 0, "organization ID")
	fs.Parse(args)
	if *orgID == 0 {
		die("--org-id is required")
	}
	resp := send(socketPath, request{Cmd: "list-members", OrgID: *orgID})
	if !resp.OK {
		die("%s", resp.Error)
	}
	var members []member
	json.Unmarshal(resp.Data, &members)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USER_ID\tUSERNAME\tCREATED")
	for _, m := range members {
		fmt.Fprintf(w, "%d\t%s\t%s\n", m.UserID, m.Username, m.CreatedAt)
	}
	w.Flush()
}

func cmdAddMember(args []string) {
	fs := flag.NewFlagSet("add-member", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["add-member"]) }
	orgID := fs.Int("org-id", 0, "organization ID")
	username := fs.String("username", "", "username")
	fs.Parse(args)
	if *orgID == 0 || *username == "" {
		die("--org-id and --username are required")
	}
	resp := send(socketPath, request{Cmd: "add-member", OrgID: *orgID, Username: *username})
	if !resp.OK {
		die("%s", resp.Error)
	}
	fmt.Printf("added %s to organization %d\n", *username, *orgID)
}

func cmdRemoveMember(args []string) {
	fs := flag.NewFlagSet("remove-member", flag.ExitOnError)
	fs.Usage = func() { fmt.Println(commandHelp["remove-member"]) }
	orgID := fs.Int("org-id", 0, "organization ID")
	username := fs.String("username", "", "username")
	fs.Parse(args)
	if *orgID == 0 || *username == "" {
		die("--org-id and --username are required")
	}
	resp := send(socketPath, request{Cmd: "remove-member", OrgID: *orgID, Username: *username})
	if !resp.OK {
		die("%s", resp.Error)
	}
	fmt.Printf("removed %s from organization %d\n", *username, *orgID)
}
