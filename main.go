package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ahmetb/go-cursor"
	"github.com/charmbracelet/glamour"
	"golang.org/x/crypto/ssh"
	terminal "golang.org/x/term"
)

// optimize for terminals with 72 char width
//
// i haven't figured out how to get the terminal width from the ssh session, so
// for the sake of time i'm hardcoding it.
const globalTerminalWidth = 72

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

type gistCache struct {
	Expiration time.Time
	Content    string
	Rendered   string
}

type GistService struct {
	files       [][]string
	cachedGists map[string]gistCache
}

func NewGistService(files [][]string) GistService {
	return GistService{
		files:       files,
		cachedGists: map[string]gistCache{},
	}
}

func (g GistService) FileNames() []string {
	fileNames := make([]string, g.Count())

	for i, f := range g.files {
		fileNames[i] = f[0]
	}

	return fileNames
}

func (g GistService) Count() int {
	return len(g.files)
}

// returns URL if file exists, empty string if not
func (g GistService) FileURL(fileName string) string {
	var url string

	for _, f := range g.files {
		if fileName == f[0] {
			url = f[1]
		}
	}

	return url
}

func (g GistService) FileExists(fileName string) bool {
	return g.FileURL(fileName) != ""
}

func (g GistService) fetchRemoteGistContents(gistURL string) (string, error) {
	rawGistURL := gistURL + "/raw"

	resp, err := http.Get(rawGistURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func (g GistService) FileContents(fileName string) (string, error) {
	gistURL := g.FileURL(fileName)
	if gistURL == "" {
		return "", errors.New("file " + fileName + " does not exist")
	}

	var cachedGist gistCache
	if cached, exists := g.cachedGists[fileName]; exists {
		cachedGist = cached
	}

	if time.Now().After(cachedGist.Expiration) {
		content, err := g.fetchRemoteGistContents(gistURL)
		if err != nil {
			return "", fmt.Errorf("error fetching remote gist: %v", err)
		}

		cachedGist.Content = content
		cachedGist.Expiration = time.Now().Add(5 * time.Minute)
	}

	g.cachedGists[fileName] = cachedGist

	return cachedGist.Content, nil
}

func (g GistService) FileRendered(fileName string) (string, error) {
	var cachedGist gistCache
	if cached, exists := g.cachedGists[fileName]; exists {
		cachedGist = cached
	}

	// if possible, just return the prerendered stuff we have
	if time.Now().Before(cachedGist.Expiration) && cachedGist.Rendered != "" {
		return cachedGist.Rendered, nil
	}

	// else, do the whole shebang...

	raw, err := g.FileContents(fileName)
	if err != nil {
		return "", err
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(int(globalTerminalWidth-3)), // 72 default width, (-3 for space for line numbers)
		glamour.WithBaseURL(g.FileURL(fileName)),
	)
	if err != nil {
		return "", err
	}

	rendered, err := r.Render(raw)
	if err != nil {
		return "", err
	}

	// custom formatting changes

	var content string
	lines := strings.Split(string(rendered), "\n")

	for i, l := range lines {
		// remove first and last two lines (which are blank)
		if i == 0 || i >= len(lines)-2 {
			continue
		}

		// add line numbers (and left pad them)
		content += fmt.Sprintf("%2v.", i) + l

		// add new lines where needed
		if i+1 < len(lines) {
			content += "\n"
		}
	}

	// change escaped \- to just - (for the signature at the end of the JDs)
	content = strings.ReplaceAll(content, `\-`, "-")

	cachedGist.Rendered = content

	g.cachedGists[fileName] = cachedGist

	return cachedGist.Rendered, nil
}

func main() {
	var sshPort string

	envSshPort := os.Getenv("SSH_PORT")
	if envSshPort == "" {
		sshPort = ":9999"
	} else {
		sshPort = ":" + envSshPort
	}

	files := [][]string{
		[]string{"README.md", "https://gist.github.com/zachlatta/3a5d780da6a3c964677a4f1c4c751f5c"},
		[]string{"game_designer.md", "https://gist.github.com/zachlatta/a00579cabbd94c98561377eaf369e9a6"},
	}

	gists := NewGistService(files)

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
						"...c..o..n..n..e..c..t..i..n..g...\r",
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
						"\r..........................................................\n\r",
						"\n\r",
						"    (ﾉ◕ヮ◕)ﾉ*:･ﾟ✧ ~*~ CONNECTED! ~*~ ✧ﾟ･: *ヽ(◕ヮ◕ヽ)\n\r",
						"\n\r",
						"..........................................................\n\r",
						"\n\r",
						"WELCOME TO THE HACK CLUB JOBS TERMINAL. PLEASE TYPE help TO BEGIN.\n\r",
						"\n\r",
					}

					typewriteLines(channel, 25*time.Millisecond, connected)

					term := terminal.NewTerminal(channel, `\(•◡•)/ ~> $ `)

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

						files := gists.FileNames()
						fileMatches := []string{}

						for _, fileName := range files {
							if strings.HasPrefix(fileName, givenFile) {
								fileMatches = append(fileMatches, fileName)
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
								fmt.Fprintln(term, `HACK CLUB JOBS TERMINAL, version 1.0.0-release (x86_64).

These shell commands are defined internally. Type `+"`help`"+` to see this
list.
`)

								// use tabwriter to neatly format command help
								helpWriter := tabwriter.NewWriter(term, 8, 8, 0, '\t', 0)

								commands := [][]string{
									[]string{"ls", "list contents of current directory"},
									[]string{"cat [file]", "display contents of current file"},
									[]string{"exit", "exit the terminal"},
								}

								for _, command := range commands {
									fmt.Fprintf(helpWriter, " %s\t%s\r\n", command[0], command[1])
								}
								helpWriter.Flush()

								fmt.Fprintln(term, "\npsst! try running 'ls' to get started")
							},
							"ls": func(args []string) {
								files := gists.FileNames()

								fmt.Fprintln(term, strings.Join(files, "\t"))
							},
							"cat": func(args []string) {
								if len(args) == 0 {
									fmt.Fprintln(term, "meow! please pass me a file! i can't do anything without one!")
									return
								}

								argFile := args[0]

								if !gists.FileExists(argFile) {
									fmt.Fprintln(term, "meow! i can't find the file", argFile)
									return
								}

								meowText := "  m e e o o o w !  "
								typewrite(term, 100*time.Millisecond, meowText)

								content, err := gists.FileRendered(argFile)
								if err != nil {
									fmt.Fprintln(term, "meow... i am having trouble accessing my brain (file retrieval error)")
									return
								}

								// clear the meow
								fmt.Fprint(term, "\r"+strings.Repeat(" ", len(meowText))+"\r")

								contentLines := strings.Split(content, "\n")

								linesToShow := 14

								exitMsg := "easier to read this file online? " + gists.FileURL(argFile) + " ~(˘▾˘~)"

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
							"exit": func(args []string) {
								goodbye := []string{
									"JOBS TERMINAL OUT. SEE YOU LATER!\r\n",
									"\nCODE AT https://github.com/hackclub/jobs\r\n",
									"\n\r(psst. did you find the easter egg?)\r\n",
									"\n(~˘▾˘)~\n\n",
								}

								typewriteLines(term, 25*time.Millisecond, goodbye)

								channel.Close()
							},
						}

						line, err := term.ReadLine()
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
