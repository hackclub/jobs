package polymer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const organization = "hackclub"

type Job struct {
	Title string `json:"title"`
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

func ListJobs() ([]Job, error) {
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

	return resp.Items, nil
}
