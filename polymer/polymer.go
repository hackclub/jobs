package polymer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/charmbracelet/glamour"
)

// optimize for terminals with 72 char width
//
// i haven't figured out how to get the terminal width from the ssh session
//
// for the sake of time, i'm hardcoding it.
const GlobalTerminalWidth = 72

const organization = "hackclub"

type Job struct {
	Id          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Url         string `json:"job_post_url"`
}

type Client struct {
	Jobs *[]Job
}

func sluggify(title string) string {
	return strings.ReplaceAll(strings.ToLower(title), " ", "-")
}

func (j Job) Slug() string {
	return sluggify(j.Title)
}

func (j Job) Filename() string {
	return j.Slug() + ".md"
}

func (j Job) Render(darkOrLight string) (string, error) {
	if darkOrLight != "light" && darkOrLight != "dark" && darkOrLight != "" {
		return "", errors.New("invalid style")
	}

	if darkOrLight == "" {
		darkOrLight = "dark"
	}

	converter := md.NewConverter("", true, nil)
	raw, err := converter.ConvertString(j.Description)
	if err != nil {
		return "", err
	}

	raw = fmt.Sprintf("# %s\n%s\n\n**Apply here!** %s", j.Title, raw, j.Url)

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(darkOrLight),
		glamour.WithWordWrap(int(GlobalTerminalWidth-3)), // 72 default width, (-3 for space for line numbers)
	)
	if err != nil {
		return "", err
	}

	rendered, err := r.Render(raw)
	if err != nil {
		return "", err
	}

	rendered = strings.TrimSpace(rendered)

	// custom formatting changes

	var content string
	lines := strings.Split(string(rendered), "\n")

	for i, l := range lines {
		// add line numbers (and left pad them)
		content += fmt.Sprintf("%2v.", i+1) + l

		// add new lines where needed
		if i+1 < len(lines) {
			content += "\n"
		}
	}

	// change escaped \- to just - (for the signature at the end of the JDs)
	content = strings.ReplaceAll(content, `\-`, "-")

	return content, nil
}

func doRequest(request *http.Request, v interface{}) error {
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("error status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if v != nil {
		err = json.Unmarshal(body, v)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) ListJobs() ([]Job, error) {
	if c.Jobs != nil {
		fmt.Println("fetching jobs from cache")
		return *c.Jobs, nil
	}

	request, err := http.NewRequest("GET", fmt.Sprintf("https://api.polymer.co/v1/hire/organizations/%s/jobs", organization), nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Items []Job `json:"items"`
	}

	err = doRequest(request, &resp)
	if err != nil {
		return nil, err
	}

	c.Jobs = &resp.Items

	return resp.Items, nil
}

func (c *Client) FetchJob(slug string) (Job, error) {
	jobs, err := c.ListJobs()
	if err != nil {
		return Job{}, err
	}

	for index, job := range jobs {
		if job.Slug() == slug {
			if job.Description == "" {
				// fetch the job's description
				request, err := http.NewRequest("GET", fmt.Sprintf("https://api.polymer.co/v1/hire/organizations/%s/jobs/%d", organization, job.Id), nil)
				if err != nil {
					return Job{}, err
				}

				var fetchedJob Job

				err = doRequest(request, &fetchedJob)
				if err != nil {
					return Job{}, err
				}

				(*c.Jobs)[index].Description = fetchedJob.Description
				job.Description = fetchedJob.Description
			}

			return job, nil
		}
	}

	return Job{}, fmt.Errorf("job not found")
}
