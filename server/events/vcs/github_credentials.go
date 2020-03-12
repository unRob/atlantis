package vcs

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v28/github"
)

// GithubCredentials handles creating http.Clients that authenticate
type GithubCredentials interface {
	Client() (*http.Client, error)
}

// GithubUserCredentials implements GithubCredentials for the personal auth token flow
type GithubUserCredentials struct {
	User  string
	Token string
}

func (c *GithubUserCredentials) Client() (*http.Client, error) {
	tr := &github.BasicAuthTransport{
		Username: strings.TrimSpace(c.User),
		Password: strings.TrimSpace(c.Token),
	}
	return tr.Client(), nil
}

// GithubAppCredentials implements GithubCredentials for github app installation token flow
type GithubAppCredentials struct {
	AppID   int64
	KeyPath string
}

func (c *GithubAppCredentials) getInstallationID() (int64, error) {
	tr := http.DefaultTransport
	// A non-installation transport
	t, err := ghinstallation.NewAppsTransportKeyFromFile(tr, c.AppID, c.KeyPath)
	if err != nil {
		return 0, err
	}

	// Query github with the app's JWT
	client := github.NewClient(&http.Client{Transport: t})
	ctx := context.Background()
	app := &struct {
		ID string `json:"id"`
	}{}
	req, err := http.NewRequest("GET", "/app", nil)
	if err != nil {
		return 0, err
	}

	_, err = client.Do(ctx, req, app)
	if err != nil {
		return 0, err
	}

	return strconv.ParseInt(app.ID, 10, 64)
}

func (c *GithubAppCredentials) Client() (*http.Client, error) {

	installationID, err := c.getInstallationID()
	if err != nil {
		return nil, err
	}

	tr := http.DefaultTransport
	itr, err := ghinstallation.NewKeyFromFile(tr, c.AppID, installationID, c.KeyPath)
	if err != nil {
		return nil, err
	}

	return &http.Client{Transport: itr}, nil
}
