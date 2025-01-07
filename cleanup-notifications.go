package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
)

type Config struct {
	token       string
	daysToCheck int
	isManualRun bool
	dryRun      bool
}

type Cleaner struct {
	client     *github.Client
	httpClient *http.Client
	ctx        context.Context
	config     Config
}

func daysToDate(days int) time.Time {
	return time.Now().Add(-time.Duration(days) * 24 * time.Hour)
}

func lessThanHours(updatedAt time.Time, hours int) bool {
	diffHours := time.Since(updatedAt).Hours()
	return diffHours < float64(hours)
}

func githubCLIToken() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub CLI token: %v", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func parseFlags() (Config, error) {
	token := flag.String("token", "", "GitHub API token")
	manual := flag.Bool("manual", false, "Run in manual mode (14 days instead of 3)")
	noDryRun := flag.Bool("no-dry-run", false, "Run without making any changes")
	daysToCheck := flag.Int("days", 3, "Number of days to check for notifications")
	flag.Parse()

	// Check token from flag or environment
	tokenVal := *token
	if tokenVal == "" {
		tokenVal = os.Getenv("GITHUB_TOKEN")
		if tokenVal == "" {
			// Try getting token from GitHub CLI
			if ghToken, err := githubCLIToken(); err == nil {
				tokenVal = ghToken
			} else {
				return Config{}, fmt.Errorf("GitHub token is required via -token flag, GITHUB_TOKEN environment variable, or GitHub CLI authentication")
			}
		}
	}

	return Config{
		token:       tokenVal,
		isManualRun: *manual,
		dryRun:      !*noDryRun,
		daysToCheck: *daysToCheck,
	}, nil
}

func newCleaner(config Config) (*Cleaner, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	return &Cleaner{
		client:     client,
		httpClient: tc,
		ctx:        ctx,
		config:     config,
	}, nil
}

func (c *Cleaner) processStateCheck(notification *github.Notification) (bool, error) {
	notificationType := notification.GetSubject().GetType()
	if notificationType != "Issue" && notificationType != "PullRequest" {
		return false, nil
	}

	url := notification.GetSubject().GetURL()
	if url == "" {
		return false, nil
	}

	issueURL := strings.Replace(strings.Replace(url, "api.", "", 1), "repos/", "", 1)
	parts := strings.Split(issueURL, "/")
	if len(parts) < 5 {
		return false, nil
	}

	owner := parts[3]
	repo := parts[4]
	number, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return false, fmt.Errorf("error parsing issue/PR number: %v", err)
	}

	var state string
	if notificationType == "Issue" {
		issue, _, err := c.client.Issues.Get(c.ctx, owner, repo, number)
		if err == nil && issue != nil {
			state = issue.GetState()
		}
	} else {
		pr, _, err := c.client.PullRequests.Get(c.ctx, owner, repo, number)
		if err == nil && pr != nil {
			state = pr.GetState()
		}
	}

	return state == "closed", nil
}

func (c *Cleaner) processNotification(notification *github.Notification) error {
	done := false
	unread := notification.GetUnread()
	updatedAt := notification.GetUpdatedAt()

	// Mark as done if notification is already read
	if !unread {
		fmt.Printf("READ  [%s] %s\n", notification.GetRepository().GetFullName(), notification.GetSubject().GetTitle())
		done = true
	}

	// Check status for Issues and PRs
	if !done {
		closed, err := c.processStateCheck(notification)
		if err != nil {
			return fmt.Errorf("error checking state: %v", err)
		}
		if closed {
			fmt.Printf("CLOSED [%s] %s\n", notification.GetRepository().GetFullName(), notification.GetSubject().GetTitle())
			done = true
		}
	}

	// Clean up URL for display
	url := notification.GetSubject().GetURL()
	if url == "" {
		url = notification.GetURL()
	}
	url = strings.Replace(strings.Replace(url, "api.", "", 1), "repos/", "", 1)

	// Process the notification
	if done {
		if lessThanHours(updatedAt.Time, 3) {
			fmt.Printf("MARK  [%s] %s  - %s\n",
				notification.GetRepository().GetFullName(),
				notification.GetSubject().GetTitle(),
				url)

			if !c.config.dryRun {
				_, err := c.client.Activity.MarkThreadRead(c.ctx, notification.GetID())
				if err != nil {
					return fmt.Errorf("error marking thread as read: %v", err)
				}
			}
		} else {
			if !c.config.dryRun {
				fmt.Printf("DONE  [%s] %s - %s\n",
					notification.GetRepository().GetFullName(),
					notification.GetSubject().GetTitle(),
					url)
				id, err := strconv.Atoi(notification.GetID())
				if err != nil {
					return fmt.Errorf("error parsing notification ID: %v", err)
				}
				_, err = c.client.Activity.MarkThreadDone(c.ctx, int64(id))
				if err != nil {
					return fmt.Errorf("error deleting thread: %v", err)
				}
			}
		}
	} else {
		fmt.Printf("SKIP  [%s] %s - %s\n",
			notification.GetRepository().GetFullName(),
			notification.GetSubject().GetTitle(),
			url)
	}

	return nil
}

func (c *Cleaner) run() error {
	// Calculate dates
	since := daysToDate(c.config.daysToCheck)
	if c.config.isManualRun {
		since = daysToDate(14)
	}

	// Log initial information
	fmt.Println("🧹 Cleaning up notifications")
	fmt.Printf("📡 Run type: %s\n", map[bool]string{true: "manual", false: "scheduled"}[c.config.isManualRun])
	if c.config.dryRun {
		fmt.Println("🔍 DRY RUN - No changes will be made")
	}
	fmt.Printf("📅 Current date: %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("📅 Since: %s\n", since.Format(time.RFC3339))

	// List notifications
	opt := &github.NotificationListOptions{
		All:   true,
		Since: since,
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	var notifications []*github.Notification
	for {
		page, resp, err := c.client.Activity.ListNotifications(c.ctx, opt)
		if err != nil {
			return fmt.Errorf("error fetching notifications: %v", err)
		}
		notifications = append(notifications, page...)

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	var errors []error
	for _, notification := range notifications {
		if err := c.processNotification(notification); err != nil {
			errors = append(errors, err)
			fmt.Printf("Error processing notification: %v\n", err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during processing", len(errors))
	}

	return nil
}

func main() {
	config, err := parseFlags()
	if err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}

	cleaner, err := newCleaner(config)
	if err != nil {
		log.Printf("Error creating cleaner: %v", err)
		os.Exit(1)
	}

	if err := cleaner.run(); err != nil {
		log.Printf("Error running cleanup: %v", err)
		os.Exit(1)
	}
}
