package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alesr/file-util"
	"golang.org/x/crypto/ssh"
)

// A project is made of project fields which has a program on it.
type program struct {
	setup              []string
	postUpdateFilename string
}

type projectField struct {
	name, label, inputQuestion, errorMsg, validationMsg string
	program                                             program
}

// Project - Defines a K project.
type Project struct {
	projectname, host, pwd, port, typ, sshkey projectField
}

var sep = string(filepath.Separator)

func main() {

	// Initialization
	project := new(Project)
	project.newProject()

	mode := mode()

	if mode == "new project" {
		project.connect()
		fmt.Printf("Project successfully created.")
	} else {
		project.insertSshkey()
		project.makeDirOnLocal()
		project.gitOnLocal()
		fmt.Println("User successfully added.")
	}
}

func (p *Project) newProject() {

	// project name
	p.projectname.inputQuestion = "project name: "
	p.projectname.label = "projectname"
	p.projectname.errorMsg = "error getting the project's name: "
	p.projectname.validationMsg = "make sure you type a valid name for your project (3 to 20 characters)."
	p.projectname.name = checkInput(ask4Input(&p.projectname))

	// Hostname
	p.host.inputQuestion = "hostname: "
	p.host.label = "hostname"
	p.host.errorMsg = "error getting the project's hostname: "
	p.host.validationMsg = "make sure you type a valid hostname for your project. it must contain '.com', '.pt' or '.org', for example.)."
	p.host.name = checkInput(ask4Input(&p.host))

	// Password
	p.pwd.inputQuestion = "password: "
	p.pwd.label = "pwd"
	p.pwd.errorMsg = "error getting the project's password: "
	p.pwd.validationMsg = "type a valid password. It must contain at least 6 digits"
	p.pwd.name = checkInput(ask4Input(&p.pwd))

	// Port
	p.port.inputQuestion = "port (default 22): "
	p.port.label = "port"
	p.port.errorMsg = "error getting the project's port"
	p.port.validationMsg = "only digits allowed. min 0, max 9999."
	p.port.name = checkInput(ask4Input(&p.port))

	// Type
	p.typ.inputQuestion = "[1] Yii\n[2] WP or goHugo\nEnter project type: "
	p.typ.label = "type"
	p.typ.errorMsg = "error getting the project's type"
	p.typ.validationMsg = "pay attention to the options"
	p.typ.name = checkInput(ask4Input(&p.typ))

	p.sshkey.inputQuestion = "Public ssh key name: "
	p.sshkey.label = "sshkey"
	p.sshkey.errorMsg = "error getting the key name"
	p.sshkey.validationMsg = "pay attention to the options"
	p.sshkey.name = checkInput(ask4Input(&p.sshkey))

	// Now we need to know which instalation we going to make.
	// And once we get to know it, let's load the setup with
	// the aproppriate set of files and commands.
	if p.typ.name == "Yii" {

		// Loading common steps into the selected setup
		p.typ.program.setup = []string{}
		p.typ.program.postUpdateFilename = "post-update-yii"
	} else {
		// Loading common steps into the selected setup
		p.typ.program.setup = []string{
			"echo -e '[User]\nname = Pipi, server girl' > .gitconfig",
			"cd ~/www/www/ && git init",
			"cd ~/www/www/ && touch readme.txt && git add . ",
			"cd ~/www/www/ && git commit -m 'on the beginning was the commit'",
			"cd ~/private/ && mkdir repos && cd repos && mkdir " + p.projectname.name + "_hub.git && cd " + p.projectname.name + "_hub.git && git --bare init",
			"cd ~/www/www && git remote add hub ~/private/repos/" + p.projectname.name + "_hub.git && git push hub master",
			"post-update configuration",
			"cd ~/www/www && git remote add hub ~/private/repos/" + p.projectname.name + "_hub.git/hooks && chmod 755 post-update",
			p.projectname.name + ".dev",
			"git clone on " + p.projectname.name + ".dev",
			"copying ssh public key",
		}
		p.typ.program.postUpdateFilename = "post-update-wp"
	}
}

// Takes the assemblyLine's data and mount the prompt for the user.
func ask4Input(field *projectField) (*projectField, string) {

	fmt.Print(field.inputQuestion)

	var input string
	_, err := fmt.Scanln(&input)

	// The port admits empty string as user input. Setting the default value to "22".
	if err != nil && err.Error() == "unexpected newline" && field.label != "port" {
		ask4Input(field)
	} else if err != nil && err.Error() == "unexpected newline" {
		input = "22"
		checkInput(field, input)
	} else if err != nil {
		log.Fatal(field.errorMsg, err)
	}
	return field, input
}

// Check invalid parameters on the user input.
func checkInput(field *projectField, input string) string {

	switch inputLength := len(input); field.label {
	case "projectname":
		if inputLength > 20 {
			fmt.Println(field.validationMsg)
			ask4Input(field)
		}
	case "hostname":
		if inputLength <= 5 {
			fmt.Println(field.validationMsg)
			ask4Input(field)
		}
	case "pwd":
		if inputLength <= 6 {
			fmt.Println(field.validationMsg)
			ask4Input(field)
		}
	case "port":
		if inputLength == 0 {
			input = "22"
		} else if inputLength > 4 {
			fmt.Println(field.validationMsg)
			ask4Input(field)
		}
	case "type":
		if input != "1" && input != "2" {
			fmt.Println(field.validationMsg)
			ask4Input(field)
		} else if input == "1" {
			input = "Yii"
		} else if input == "2" {
			input = "WP"
		}
	}

	// Everything looks fine so lets set the value.
	return input
}

// Creates a ssh connection between the local machine and the remote server.
func (p *Project) connect() {

	ticker := time.NewTicker(time.Millisecond * 800)
	go func() {
		for _ = range ticker.C {
			fmt.Println("Trying connection...")
		}
	}()
	time.Sleep(time.Millisecond * 1500)

	conn := p.dial()
	ticker.Stop()
	fmt.Println("Connection established.")

	session, err := conn.NewSession()
	if err != nil {
		log.Fatal("Failed to build session: ", err)
	}

	defer session.Close()

	// Loops over the slice of commands to be executed on the remote.
	for step := range p.typ.program.setup {

		switch p.typ.program.setup[step] {

		case "post-update configuration":
			filepath := "post-update-files" + sep + p.typ.program.postUpdateFilename
			p.secureCopy(conn, "post-update configuration", filepath)

		case p.projectname.name + ".dev":
			p.makeDirOnLocal()

		case "git clone on " + p.projectname.name + ".dev":
			p.gitOnLocal()

		case "copying ssh public key":
			userHomeDir, err := fileUtil.FindUserHomeDir()
			if err != nil {
				log.Fatal("Failed to find user home directory: ", err)
			}

			filepath := userHomeDir + sep + ".ssh/" + p.sshkey.name + ".pub"
			p.secureCopy(conn, "copying ssh public key", filepath)

		default:
			p.installOnRemote(step, conn)
		}
	}
}

func (p *Project) dial() *ssh.Client {
	// SSH connection config
	config := &ssh.ClientConfig{
		User: p.projectname.name,
		Auth: []ssh.AuthMethod{
			ssh.Password(p.pwd.name),
		},
	}
	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", p.host.name, p.port.name), config)
	if err != nil {
		log.Println("Failed to dial: ", err)
		log.Println("Trying again...")
		p.connect()
	}
	return conn
}

func (p *Project) installOnRemote(step int, conn *ssh.Client) {

	// Git and some other programs can send us an unsuccessful exit (< 0)
	// even if the command was successfully executed on the remote shell.
	// On these cases, we want to ignore those errors and move onto the next step.
	ignoredError := "Reason was:  ()"

	// Creates a session over the ssh connection to execute the commands
	session, err := conn.NewSession()
	if err != nil {
		log.Fatal("Failed to build session: ", err)
	}

	defer session.Close()

	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf

	fmt.Println(p.typ.program.setup[step])

	err = session.Run(p.typ.program.setup[step])

	if err != nil && !strings.Contains(err.Error(), ignoredError) {
		log.Printf("Command '%s' failed on execution", p.typ.program.setup[step])
		log.Fatal("Error on command execution: ", err.Error())
	}
}

// Secure Copy a file from local machine to remote host.
func (p *Project) secureCopy(conn *ssh.Client, phase, filepath string) {
	session, err := conn.NewSession()
	if err != nil {
		log.Fatal("Failed to build session: ", err)
	}

	defer session.Close()

	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf

	var file, dest string
	if phase == "post-update configuration" {
		file = "post-update"
		dest = "scp -qrt ~/private/repos/" + p.projectname.name + "_hub.git/hooks"
	} else {
		file = "authorized_keys"
		dest = "scp -qrt ~" + sep + ".ssh"
	}

	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		content, err := fileUtil.ReadFile(filepath)
		if err != nil {
			log.Fatalf("Failed to read file on %s: %e", filepath, err)
		}
		fmt.Fprintln(w, "C0644", len(content), file)
		fmt.Fprint(w, content)
		fmt.Fprint(w, "\x00")
	}()

	fmt.Printf("%s... %s\n", file, dest)

	ignoredError := "Reason was:  ()"
	if err := session.Run(dest); err != nil && !strings.Contains(err.Error(), ignoredError) {
		log.Fatal("Failed to run SCP: " + err.Error())
	}
}

// Creates a directory on the local machine. Case the directory already exists
// remove the old one and runs the function again.
func (p *Project) makeDirOnLocal() {

	fmt.Println("Creating directory...")

	// Get the user home directory path.
	homeDir, err := fileUtil.FindUserHomeDir()
	if err != nil {
		log.Fatalf("Failed to find user home directory: %e", err)
	}

	// The dir we want to create.
	dir := homeDir + sep + "sites" + sep + p.projectname.name + ".dev"

	// Check if the directory already exists.
	if _, err := os.Stat(dir); os.IsNotExist(err) {

		err := os.Mkdir(dir, 0755)
		if err != nil {
			log.Fatal("Failed to create directory: ", err)
		}

		fmt.Println(dir + " successfully created.")

	} else {
		fmt.Println(dir + " already exist.\nRemoving old and creating new...")

		// Remove the old one.
		if err := os.RemoveAll(dir); err != nil {
			log.Fatalf("Error removing %s\n%s", dir, err)
		}

		p.makeDirOnLocal()
	}
}

// Git clone on local machine
func (p *Project) gitOnLocal() {

	homeDir, err := fileUtil.FindUserHomeDir()
	if err != nil {
		log.Fatal("Failed to find user home directory: ", err)
	}

	if err := os.Chdir(homeDir + sep + "sites" + sep + p.projectname.name + ".dev" + sep); err != nil {
		log.Fatal("Failed to change directory.")
	} else {
		repo := "ssh://" + p.projectname.name + "@" + p.host.name + "/home/" + p.projectname.name + "/private/repos/" + p.projectname.name + "_hub.git"
		fmt.Println("Cloning repository...")

		wg := new(sync.WaitGroup)
		wg.Add(3)

		exeCmd("git clone "+repo+" .", wg)
		exeCmd("git remote rename origin hub", wg)
	}
}

func printLocalCmdOutput(out []byte) {
	if len(out) > 0 {
		fmt.Println(out)
	}
}

func mode() string {

	var input, mode string

	fmt.Print("[1] Add new project\n[2] Add user to an existing project\nSelect mode: ")

	_, err := fmt.Scanln(&input)
	if err != nil {
		log.Fatal("Failed to read user input: ", err)
	}

	if input == "1" {

		var confirmation string

		fmt.Print("Be aware that setting a new project will overwrite all previous environment configuration.\nDo you want to continue? [Y] [N]: ")

		_, err := fmt.Scanln(&confirmation)
		if err != nil {
			log.Fatal("Failed to read user input: ", err)
		}

		if confirmation == strings.ToLower("y") || confirmation == strings.ToLower("yes") {
			mode = "new project"
		} else {
			fmt.Println("Exiting program...")
			os.Exit(0)
		}

	} else if input == "2" {
		mode = "new user"
	} else {
		fmt.Println("Please type 1 or 2 to select the options.")
		main()
	}
	return mode
}

func (p *Project) insertSshkey() {

	fmt.Println("Copying public key...")

	homeDir, err := fileUtil.FindUserHomeDir()
	if err != nil {
		log.Fatalf("Failed to find user home directory: %e", err)
	}

	keyFile, err := os.Open(homeDir + sep + ".ssh" + sep + p.sshkey.name + ".pub")
	if err != nil {
		log.Fatal(err)
	}

	cmd := exec.Command("ssh", p.projectname.name+"@"+p.host.name, "cat >> ~/.ssh/authorized_keys")
	cmd.Stdin = keyFile

	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		log.Fatal(err)
	}
}

func exeCmd(cmd string, wg *sync.WaitGroup) {

	// splitting head => g++ parts => rest of the command
	parts := strings.Fields(cmd)
	head := parts[0]
	parts = parts[1:len(parts)]

	out, err := exec.Command(head, parts...).Output()
	if err != nil {
		fmt.Printf("%s", err)
	}
	fmt.Printf("%s", out)

	wg.Done() // Need to signal to waitgroup that this goroutine is done
}
