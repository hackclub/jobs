package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"golang.org/x/crypto/ssh"
	terminal "golang.org/x/term"
)

type Session struct {
	Width    int
	Height   int
	Terminal *terminal.Terminal
}

func main() {
	var sshPort string

	envSshPort := os.Getenv("SSH_PORT")
	if envSshPort == "" {
		sshPort = ":9999"
	} else {
		sshPort = ":" + envSshPort
	}

	config := &ssh.ServerConfig{
		NoClientAuth: true,
	}

	privateBytes, err := ioutil.ReadFile("tmp/id_rsa")
	if err != nil {
		panic("Failed to open private key from disk")
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		panic("Failed to parse private key")
	}

	config.AddHostKey(private)

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0%s", sshPort))
	if err != nil {
		panic("failed to listen for connection")
	}

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
						"...connecting...\r",
						"...c.o.n.n.e.c.t.i.n.g...\r",
						"...c..o..n..n..e..c..t..i..n..g...\r",
						"...c...o...n...n...e....",
					}

					connectingSpeed := 100

					for _, l := range connecting {
						for _, c := range strings.Split(l, "") {
							fmt.Fprint(channel, c)
							time.Sleep(time.Duration(connectingSpeed) * time.Millisecond)
						}

						connectingSpeed += 25
					}

					fmt.Fprint(channel, "just kidding")

					time.Sleep(1 * time.Second)

					connected := []string{
						"\r..........................................................\n\r",
						"\n\r",
						"    (ﾉ◕ヮ◕)ﾉ*:･ﾟ✧ ~*~ CONNECTED! ~*~ ✧ﾟ･: *ヽ(◕ヮ◕ヽ)\n\r",
						"\n\r",
						"..........................................................\n\r",
						"\n\r",
						"WELCOME TO THE HACK CLUB JOBS TERMINAL. PLEASE TYPE help TO BEGIN.\n\r",
						"\n\r",
					}

					for _, l := range connected {
						for _, c := range strings.Split(l, "") {
							fmt.Fprint(channel, c)
							time.Sleep(25 * time.Millisecond)
						}
					}

					term := terminal.NewTerminal(channel, `\(•◡•)/ ~> $ `)

					session := Session{
						Width:    80, // hardcoded for now
						Height:   42, // same here
						Terminal: term,
					}

					for {
						files := [][]string{
							[]string{"README.md", "https://gist.github.com/zachlatta/3a5d780da6a3c964677a4f1c4c751f5c"},
							[]string{"game_designer.md", "https://gist.github.com/zachlatta/a00579cabbd94c98561377eaf369e9a6"},
						}

						cmds := map[string]func([]string){
							"help": func(args []string) {
								fmt.Fprintln(term, `HACK CLUB JOBS TERMINAL, version 1.0.0-release (x86_64).

These shell commands are defined internally. Type `+"`help`"+` to see this list.

 ls		list contents of current directory
 cat [file]	display contents of current file
 
psst! try running 'ls' to get started`)
							},
							"ls": func(args []string) {
								fileNames := make([]string, len(files))

								for i, f := range files {
									fileNames[i] = f[0]
								}

								fmt.Fprintln(term, strings.Join(fileNames, "\t"))
							},
							"cat": func(args []string) {
								if len(args) == 0 {
									fmt.Fprintln(term, "meow! please pass me a file! i can't do anything without one!")
									return
								}

								argFile := args[0]

								var file []string

								for _, f := range files {
									if argFile == f[0] {
										file = f
									}
								}

								if file == nil {
									fmt.Fprintln(term, "meow! i can't find the file", argFile)
									return
								}

								meowText := "  m e e o o o w !  "

								for _, c := range strings.Split(meowText, "") {
									fmt.Fprint(term, c)
									time.Sleep(100 * time.Millisecond)
								}

								time.Sleep(1500 * time.Millisecond)

								fmt.Fprint(term, "\r"+strings.Repeat(" ", len(meowText))+"\r")

								rawGistURL := file[1] + "/raw"

								resp, err := http.Get(rawGistURL)
								if err != nil {
									fmt.Fprintln(term, "gosh, i'm really sorry but my wires seem to be crossed. try that again?")
									return
								}
								defer resp.Body.Close()
								body, err := ioutil.ReadAll(resp.Body)
								if err != nil {
									fmt.Fprintln(term, "gosh, i'm really sorry but my wires seem to be shorting. try that again?")
									return
								}

								r, err := glamour.NewTermRenderer(
									glamour.WithEnvironmentConfig(),
									glamour.WithWordWrap(int(session.Width)),
									glamour.WithBaseURL(file[1]),
								)
								if err != nil {
									fmt.Fprintln(term, "something bad happened with my glasses, sorry")
								}

								rendered, err := r.RenderBytes(body)
								if err != nil {
									fmt.Fprintln(term, "i tried to make it all pretty for you, but i'm having trouble!")
									return
								}

								var content string
								lines := strings.Split(string(rendered), "\n")

								for i, l := range lines {
									// remove first and last two lines (which are blank)
									if i == 0 || i >= len(lines)-2 {
										continue
									}

									content += fmt.Sprintf("%2v.", i) + l

									// add new lines where needed
									if i+1 < len(lines) {
										content += "\n"
									}
								}

								// Change escaped \- to just - (for the signature at the end of the JDs)
								content = strings.ReplaceAll(content, `\-`, "-")

								contentLines := strings.Split(content, "\n")

								linesToShow := 12
								secondsToWait := 15

								if len(contentLines) <= linesToShow {
									fmt.Fprint(term, content)

									fmt.Fprint(term, "\n\n(easier to read this file online? "+file[1]+")")
									return
								}

								fmt.Fprintln(term, strings.Join(contentLines[:linesToShow], "\n"))

								fmt.Fprint(term, "~ printing more in "+fmt.Sprint(secondsToWait)+"... ~")

								for secondsToWait != 0 {
									time.Sleep(1 * time.Second)

									secondsToWait--

									fmt.Fprint(term, "\r~ printing more in "+fmt.Sprint(secondsToWait)+"... ~ ")
								}

								fmt.Fprint(term, "\r"+strings.Join(contentLines[linesToShow:], "\n"))
							},
						}

						line, err := session.Terminal.ReadLine()
						if err != nil {
							break
						}

						trimmedInput := strings.TrimSpace(line)

						inputElements := strings.Split(trimmedInput, " ")
						inputCmd := inputElements[0]
						inputArgs := inputElements[1:]

						if cmd, ok := cmds[inputCmd]; ok {
							fmt.Fprintln(term, "")
							cmd(inputArgs)
							fmt.Fprintln(term, "")
						} else {
							fmt.Fprintln(term, "")
							fmt.Fprintln(term, inputCmd, "is not a known command.")
							fmt.Fprintln(term, "")
						}
					}
				}()
			}
		}()
	}
}
