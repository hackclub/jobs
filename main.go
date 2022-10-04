package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ahmetb/go-cursor"
	"github.com/hackclub/jobs/polymer"
	"golang.org/x/crypto/ssh"
	terminal "golang.org/x/term"
)

func typewrite(w io.Writer, speed time.Duration, content string) {
	chars := strings.Split(content, "")

	for _, c := range chars {
		fmt.Fprint(w, c)
		time.Sleep(speed)
	}
}

func typewriteLines(w io.Writer, speed time.Duration, lines []string) {
	for _, line := range lines {
		typewrite(w, speed, line)
	}
}

func main() {
	var sshPort string

	envSshPort := os.Getenv("SSH_PORT")
	if envSshPort == "" {
		sshPort = ":9999"
	} else {
		sshPort = ":" + envSshPort
	}

	polymerClient := polymer.Client{}

	config := &ssh.ServerConfig{
		NoClientAuth: true,
	}

	// create /tmp if it doesn't exist
	if _, err := os.Stat("tmp/"); os.IsNotExist(err) {
		os.Mkdir("tmp/", os.ModeDir)
	}

	// add ed25519 key
	privateBytes25519, err := ioutil.ReadFile("tmp/id_ed25519")
	if err != nil {
		panic("Failed to open private key from disk. Try running `ssh-keygen -t ed25519` in tmp/ to create one.")
	}
	private25519, err := ssh.ParsePrivateKey(privateBytes25519)
	if err != nil {
		panic("Failed to parse ed25519 private key")
	}
	config.AddHostKey(private25519)

	// add rsa key
	privateBytesRSA, err := ioutil.ReadFile("tmp/id_rsa")
	if err != nil {
		panic("Failed to open private key from disk. Try running `ssh-keygen` in tmp/ to create one.")
	}
	privateRSA, err := ssh.ParsePrivateKey(privateBytesRSA)
	if err != nil {
		panic("Failed to parse rsa private key")
	}
	config.AddHostKey(privateRSA)

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0%s", sshPort))
	if err != nil {
		panic("failed to listen for connection")
	}

	fmt.Println("SSH server running at 0.0.0.0" + sshPort)

	for {
		nConn, err := listener.Accept()
		if err != nil {
			panic("failed to accept incoming connection")
		}

		go func() {
			// ssh handshake must be performed
			_, chans, reqs, err := ssh.NewServerConn(nConn, config)
			if err != nil {
				fmt.Println("failed to handshake with new client:", err)
				return
			}

			// ssh connections can make "requests" outside of the main tcp pipe
			// for the connection. receive and discard all of those.
			go ssh.DiscardRequests(reqs)

			for newChannel := range chans {
				if newChannel.ChannelType() != "session" {
					newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
					continue
				}

				channel, requests, err := newChannel.Accept()
				if err != nil {
					fmt.Println("could not accept channel:", err)
					return
				}

				go func(in <-chan *ssh.Request) {
					for req := range in {
						if req.Type == "shell" {
							req.Reply(true, nil)
						}
					}
				}(requests)

				go func() {
					defer channel.Close()

					connecting := []string{
						"\x1b[33m...connecting...\x1b[0m\r",
						"\x1b[35m...c..o..n..n..e..c..t..i..n..g...\x1b[0m\r",
					}

					connectingSpeed := 100

					for _, l := range connecting {
						for _, c := range strings.Split(l, "") {
							fmt.Fprint(channel, c)
							time.Sleep(time.Duration(connectingSpeed) * time.Millisecond)
						}

						connectingSpeed += 50
					}

					connected := []string{
						"\r\x1b[2m..........................................................\x1b[0m\n\r",
						"\n\r",
						"    \x1b[35m(ﾉ◕ヮ◕)ﾉ*:･ﾟ✧ ~*~ CONNECTED! ~*~ ✧ﾟ･: *ヽ(◕ヮ◕ヽ)\x1b[0m\n\r",
						"\n\r",
						"\x1b[2m..........................................................\x1b[0m\n\r",
						"\n\r",
						"\x1b[1mWELCOME TO THE HACK CLUB JOBS TERMINAL.\x1b[0m PLEASE TYPE `help` TO BEGIN.\n\r",
						"\n\r",
					}

					typewriteLines(channel, 25*time.Millisecond, connected)

					term := terminal.NewTerminal(channel, "\x1b[36m\\(•◡•)/ ~> \x1b[1m$\x1b[0m ")

					term.AutoCompleteCallback = func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
						// only autocomplete when they hit tab
						if key != '\t' {
							return newLine, newPos, ok
						}

						lineParts := strings.Split(line, " ")

						// only autocomplete if they're typing a file into cat
						if lineParts[0] != "cat" {
							return newLine, newPos, ok
						}

						var givenFile string
						if len(lineParts) > 1 {
							givenFile = lineParts[1]
						}

						jobs, err := polymerClient.ListJobs()
						if err != nil {
							return newLine, newPos, ok
						}

						files := []string{"README.md"}

						for _, job := range jobs {
							files = append(files, job.Filename())
						}

						fileMatches := []string{}

						for _, file := range files {
							if strings.HasPrefix(file, givenFile) {
								fileMatches = append(fileMatches, file)
							}
						}

						if len(fileMatches) > 1 {
							fmt.Fprintln(term, strings.Join(fileMatches, "\t")+"\n")
						} else if len(fileMatches) == 1 {
							newLine = strings.Join([]string{"cat", fileMatches[0]}, " ")
							newPos = len(newLine)
							ok = true
						}

						return newLine, newPos, ok
					}

					for {
						cmds := map[string]func([]string){
							"help": func(args []string) {
								fmt.Fprintln(term, "\x1b[1mHACK CLUB JOBS TERMINAL\x1b[0m \x1b[2mversion 1.0.1-release (x86_64)\x1b[0m"+`
These shell commands are defined internally. Type `+"`help`"+` to see this
list.
`)

								// use tabwriter to neatly format command help
								helpWriter := tabwriter.NewWriter(term, 8, 8, 0, '\t', 0)

								commands := [][]string{
									{"ls", "list contents of current directory"},
									{"cat [file] [dark or light]", "display contents of current file"},
									{"clear", "summon the  v o i d"},
									{"exit", "exit the terminal"},
								}

								for _, command := range commands {
									fmt.Fprintf(helpWriter, " %s\t%s\r\n", command[0], command[1])
								}
								helpWriter.Flush()

								fmt.Fprintln(term, "\npsst! try running `ls` to get started")
							},
							"ls": func(args []string) {
								fmt.Fprintln(term, "\x1b[1;2myou dust off the shelves and find the following files laying about...\x1b[0m\n\r")

								jobs, err := polymerClient.ListJobs()
								if err != nil {
									log.Fatal(err)
								}

								files := []string{"\x1b[1m" + "README.md" + "\x1b[0m"}

								for _, job := range jobs {
									files = append(files, job.Filename())
								}

								fmt.Fprintln(term, strings.Join(files, "\n"))
							},
							"clear": func(args []string) {
								fmt.Fprint(term, "\x1b[H\x1b[2J")
							},
							"cat": func(args []string) {
								if len(args) == 0 {
									fmt.Fprintln(term, "meow! please pass me a file! i can't do anything without one!")
									return
								}

								argFile := strings.TrimSuffix(args[0], ".md")
								var darkOrLight string

								if len(args) > 1 {
									darkOrLight = args[1]
								}

								job, err := polymerClient.FetchJob(argFile)
								if err != nil {
									fmt.Fprintln(term, "meow! i can't find the file", argFile)
									return
								}

								meowText := "  m e e o o o w !  "
								typewrite(term, 100*time.Millisecond, meowText)

								content, err := job.Render(darkOrLight)
								if err != nil {
									fmt.Println(err)
									fmt.Fprintln(term, "meow... i am having trouble accessing my brain (file retrieval error)")
									return
								}

								// clear the meow
								fmt.Fprint(term, "\r"+strings.Repeat(" ", len(meowText))+"\r")

								contentLines := strings.Split(content, "\n")

								linesToShow := 14

								var exitMsg string

								if darkOrLight == "" || darkOrLight == "dark" {
									exitMsg += " ~ psst. you can switch to light mode with `cat [file] light` ~"
								} else {
									exitMsg += " ~ psst. you can switch to dark mode with `cat [file] dark` ~"
								}

								exitMsg += "\r\n\n easier to read this file online? " + job.Url + " ~(˘▾˘~)"

								// if we don't need to page, print and exit
								if len(contentLines) <= linesToShow {
									fmt.Fprintln(term, content)
									fmt.Fprintln(term, exitMsg)
									return
								}

								// page!
								input := make(chan string, 1)
								finishedPrinting := false

								go func() {
									fmt.Println("ATTEMPTING TO PAGE")
									totalLines := len(contentLines)
									currentLine := 0

									// print the first n lines
									fmt.Fprintln(term, strings.Join(contentLines[currentLine:linesToShow], "\n"))

									currentLine += linesToShow

									for range input {
										nextCurrentLine := currentLine + linesToShow
										if nextCurrentLine > totalLines {
											nextCurrentLine = totalLines
										}

										fmt.Fprint(term, cursor.MoveUp(1))
										fmt.Fprintln(term, strings.Join(contentLines[currentLine:nextCurrentLine], "\n"))

										currentLine = nextCurrentLine

										if currentLine >= totalLines {
											finishedPrinting = true
											break
										}
									}
								}()

								for !finishedPrinting {
									line, err := term.ReadPassword("     ~(press enter to print more...)~")
									if err != nil {
										break
									}

									input <- line
								}

								fmt.Fprint(term, cursor.MoveUp(1))

								fmt.Fprintln(term, exitMsg)
							},
							"pwd": func(args []string) {
								typewrite(term, 75*time.Millisecond, "you look up, you look down, you look all around. you are completely and utterly lost.\n\r")
							},
							"cd": func(args []string) {
								typewrite(term, 75*time.Millisecond, "what even IS a directory? this is the HACK CLUB JOBS TERMINAL. there are only jobs here.\r\n")
							},
							"whoami": func(args []string) {
								typewrite(term, 75*time.Millisecond, "who ARE you? why are we here? what IS this all about?\r\n")
							},
							"exit": func(args []string) {
								goodbye := []string{
									"\x1b[1;34mJOBS TERMINAL OUT. SEE YOU LATER!\x1b[0m\r\n",
									"CODE AT https://github.com/hackclub/jobs\r\n",
									"WANT TO TRY SOMETHING FUN? RUN $ ssh sshtron.zachlatta.com\r\n",
									"(~˘▾˘)~\n\n",
								}

								typewriteLines(term, 25*time.Millisecond, goodbye)

								channel.Close()
							},
						}

						line, err := term.ReadLine()
						if err != nil {
							break
						}

						log.Println(nConn.RemoteAddr(), "ran command:", line)

						trimmedInput := strings.TrimSpace(line)

						inputElements := strings.Split(trimmedInput, " ")
						inputCmd := inputElements[0]
						inputArgs := inputElements[1:]

						if cmd, ok := cmds[inputCmd]; ok {
							fmt.Fprintln(term, "")
							cmd(inputArgs)
							fmt.Fprintln(term, "")
						} else if inputCmd != "" {
							fmt.Fprintln(term, "")
							fmt.Fprintln(term, inputCmd, `is not a known command.
p.s. this is a custom SSH server, with a custom shell, written in Go. open source at https://github.com/hackclub/jobs!`)
							fmt.Fprintln(term, "")
						}
					}
				}()
			}
		}()
	}
}
