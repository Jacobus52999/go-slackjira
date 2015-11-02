package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nlopes/slack"
)

const (
	jiraURL      = "JIRA_URL"
	jiraUser     = "JIRA_USER"
	jiraPassword = "JIRA_PASSWORD"
	jiraIcon     = "https://globus.atlassian.net/images/64jira.png"
	slackToken   = "SLACK_TOKEN"
	issueURL     = "/rest/api/2/issue/"
	projectsURL  = "/rest/api/2/project"
	yellow       = "#FFD442"
	green        = "#048A25"
	blue         = "#496686"
)

type (
	// Project Jira project
	Project struct {
		ID  string `json:"id"`
		KEY string `json:"key"`
	}
	// JiraClient http client for connecting to the Jira server
	JiraClient struct {
		username   string
		password   string
		baseURL    *url.URL
		httpClient *http.Client
	}

	//Issue Jira issue
	Issue struct {
		Key    string
		Fields *IssueFields
	}
	//IssueFields fields for Jira issue
	IssueFields struct {
		IssueType *IssueType
		Summary   string
		Creator   *Creator
		Assignee  *Assignee
		Priority  *Priority
		Status    *Status
	}

	//IssueType Jira issue type e.g Task,Bug etc
	IssueType struct {
		IconURL string
		Name    string
	}

	//Creator Jira issue creator
	Creator struct {
		DisplayName string
	}

	//Assignee Jira issue assignee
	Assignee struct {
		DisplayName string
	}

	//Priority Jira issue priority
	Priority struct {
		Name    string
		IconURL string
	}

	//Status Jira issue status, e.g open closed
	Status struct {
		Name    string
		IconURL string
	}
)

var (
	//Pattern hold the issue regex
	Pattern *regexp.Regexp
	//Projects all of the Jira projects
	Projects = []Project{}
	//Slack slack client
	Slack *slack.Client
	//Client JiraClient
	Client            JiraClient
	jiraHostURL       string
	jiraUserName      string
	jiraiUserPassword string
	slackAPIToken     string
)

//NewClient new jira client
func NewClient(username, password string, baseURL *url.URL) JiraClient {
	return JiraClient{
		username:   username,
		password:   password,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

//GetProjects returns a representation of a Jira project for the given project key.  An example of a key is MYPROJ.
func (client JiraClient) GetProjects() error {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s", client.baseURL, projectsURL), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(client.username, client.password)

	responseCode, data, err := client.consumeResponse(req)
	if err != nil {
		return err
	}
	if responseCode != http.StatusOK {
		return fmt.Errorf("error getting project.  Status code: %d.\n", responseCode)
	}

	if err := json.Unmarshal(data, &Projects); err != nil {
		return err

	}
	return nil
}

//GetIssue serach jira for an issue
func (client JiraClient) GetIssue(issuekey string) (Issue, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s%s", client.baseURL, issueURL, issuekey), nil)
	var issue Issue
	if err != nil {
		return issue, err
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(client.username, client.password)

	responseCode, data, err := client.consumeResponse(req)
	if err != nil {
		return issue, err
	}

	if responseCode != http.StatusOK {
		return issue, fmt.Errorf("error getting project.  Status code: %d.\n", responseCode)
	}

	if err := json.Unmarshal(data, &issue); err != nil {
		return issue, err
	}
	if issue.Key == "" {
		return issue, errors.New("No Issue were found")
	}
	if issue.Fields.Assignee == nil {
		issue.Fields.Assignee = &Assignee{"Unassigned"}
	}

	return issue, nil
}
func (client JiraClient) consumeResponse(req *http.Request) (rc int, buffer []byte, err error) {
	response, err := client.httpClient.Do(req)
	if err != nil {
		return response.StatusCode, nil, err
	}
	defer response.Body.Close()

	if data, err := ioutil.ReadAll(response.Body); err == nil {
		return response.StatusCode, data, nil
	}
	return response.StatusCode, nil, err
}

func buildPattern() {
	pattern := `(?:\W|^)((`
	for _, p := range Projects {
		pattern += p.KEY
		pattern += "|"
	}
	pattern += `)-\d+)(\+)?|$`
	Pattern = regexp.MustCompile(pattern)
}

func getColor(status string) (color string) {
	switch status {
	case "Open":
		color = blue
	case "Reopened":
		color = blue
	case "To Do":
		color = blue
	case "Resolved":
		color = green
	case "Closed":
		color = green
	case "Done":
		color = green
	default:
		color = yellow

	}

	return color
}
func sendMessage(issue Issue, channel string) error {
	params := slack.PostMessageParameters{}
	text := fmt.Sprintf("*%s*\n\n *Assignee* %s *Priority* %s ", issue.Fields.Summary, issue.Fields.Assignee.DisplayName, issue.Fields.Priority.Name)
	attachment := slack.Attachment{
		Title:      issue.Key,
		TitleLink:  fmt.Sprintf("%s/browse/%s", jiraHostURL, issue.Key),
		Text:       text,
		Color:      getColor(issue.Fields.Status.Name),
		MarkdownIn: []string{"text", "pretext"},
	}
	params.Attachments = []slack.Attachment{attachment}
	params.IconURL = jiraIcon
	params.Username = "Jira"
	_, _, err := Slack.PostMessage(channel, "", params)
	if err != nil {
		fmt.Printf("%s\n", err)
		return err
	}
	return nil

}

func processEvents(text string, channel string, wg sync.WaitGroup) {
	defer wg.Done()
	matches := Pattern.FindAllStringSubmatch(text, -1)
	for _, v := range matches {
		if issue, err := Client.GetIssue(strings.TrimSpace(v[1])); err == nil {
			sendMessage(issue, channel)
		}
	}
}
func main() {
	var wg sync.WaitGroup
	jiraHostURL = os.Getenv(jiraURL)
	jiraUserName = os.Getenv(jiraUser)
	jiraiUserPassword = os.Getenv(jiraPassword)
	slackAPIToken = os.Getenv(slackToken)
	url, _ := url.Parse(jiraHostURL)
	Client = NewClient(jiraUserName, jiraiUserPassword, url)
	Slack = slack.New(slackAPIToken)
	Slack.SetDebug(false)
	Client.GetProjects()
	buildPattern()
	rtm := Slack.NewRTM()
	go rtm.ManageConnection()

Loop:
	for {
		select {
		case msg := <-rtm.IncomingEvents:
			switch ev := msg.Data.(type) {
			case *slack.MessageEvent:
				if ev.SubType != "bot_message" {
					wg.Add(1)
					go processEvents(ev.Text, ev.Channel, wg)
				}
			case *slack.InvalidAuthEvent:
				fmt.Printf("Invalid credentials")
				break Loop
			default:
				// Ignore other events..
			}
		}
	}
	wg.Wait()
}
