package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/go-github/v28/github"
	"github.com/google/go-querystring/query"
	"github.com/runatlantis/atlantis/server/logging"
)

// GithubAppController handles the creation and setup of a new GitHub app
type GithubAppController struct {
	AtlantisURL         *url.URL
	Logger              *logging.SimpleLogger
	GithubSetupComplete bool
	GithubHostname      string
	GithubOrg           string
}

// githubAppRequest contains the query parameters for
// https://developer.github.com/apps/building-github-apps/creating-github-apps-using-url-parameters/
type githubAppRequest struct {
	Name            string   `url:"name"`
	Description     string   `url:"description"`
	URL             string   `url:"url"`
	CallbackURL     string   `url:"callback_url"`
	SetupURL        string   `url:"setup_url"`
	SetupOnUpdate   bool     `url:"setup_on_update"`
	Public          bool     `url:"public"`
	WebhookURL      string   `url:"webhook_url"`
	WebhookSecret   string   `url:"webhook_secret"`
	Events          []string `url:"events"`
	Checks          string   `url:"checks"`
	Contents        string   `url:"contents"`
	Issues          string   `url:"issues"`
	PullRequests    string   `url:"pull_requests"`
	RepositoryHooks string   `url:"repository_hooks"`
	Statuses        string   `url:"statuses"`
}

// githubAppResponse is the json response sent to the user
// after a successful code exchange
type githubAppResponse struct {
	COMMENT       string `json:"_comment"`
	ID            int64  `json:"gh-app-id"`
	Key           string `json:"gp-app-key"`
	WebhookSecret []byte `json:"gh-webhook-secret"`
}

// ExchangeCode handles the user coming back from creating their app
// A code query parameter is exchanged for this app's ID, key, and webhook_secret
// Implements https://developer.github.com/apps/building-github-apps/creating-github-apps-from-a-manifest/#implementing-the-github-app-manifest-flow
func (g *GithubAppController) ExchangeCode(w http.ResponseWriter, r *http.Request) {

	if g.GithubSetupComplete {
		g.respond(w, logging.Error, http.StatusBadRequest, "Atlantis already has GitHub credentials")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		g.respond(w, logging.Debug, http.StatusOK, "Ignoring callback, missing code query parameter")
	}

	g.Logger.Debug("Exchanging GitHub app code for app credentials")
	tr := http.DefaultTransport
	client := github.NewClient(&http.Client{Transport: tr})

	ctx := context.Background()
	app := &struct {
		ID            int64  `json:"id"`
		Key           string `json:"pem"`
		WebhookSecret []byte `json:"webhook_secret"`
		Name          string `json:"name"`
	}{}
	url := fmt.Sprintf("/app-manifests/%s/conversions", code)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		g.respond(w, logging.Error, http.StatusBadGateway, "Error creating http request to exchange token: %s", err)
		return
	}

	res, err := client.Do(ctx, req, app)
	if err != nil {
		g.respond(w, logging.Error, http.StatusBadGateway, "Error exchanging code for token: %s", err)
		return
	}

	if res.StatusCode >= 400 {
		response := []byte{}
		res.Body.Read(response)
		g.respond(w, logging.Error, res.StatusCode, "Error exchanging code for token: %q", response)
		return
	}
	g.Logger.Debug("Found credentials for GitHub app %q with id %d", app.Name, app.ID)

	response, _ := json.Marshal(&githubAppResponse{
		COMMENT:       "Update these values in your Atlantis config and restart the server",
		ID:            app.ID,
		WebhookSecret: app.WebhookSecret,
		Key:           app.Key,
	})
	g.respond(w, logging.Info, http.StatusOK, string(response))
}

// New redirects the user to create a new GitHub app
func (g *GithubAppController) New(w http.ResponseWriter, r *http.Request) {

	if g.GithubSetupComplete {
		g.respond(w, logging.Error, http.StatusBadRequest, "Atlantis already has GitHub credentials")
		return
	}

	secret, err := g.newWebhookSecret(20)
	if err != nil {
		g.respond(w, logging.Error, http.StatusBadGateway, "Error initializing webhook secret: %s", err)
		return
	}

	q, _ := query.Values(&githubAppRequest{
		Name:          "Atlantis",
		Description:   fmt.Sprintf("Terraform Pull Request Automation at %s", g.AtlantisURL),
		URL:           g.AtlantisURL.String(),
		CallbackURL:   fmt.Sprintf("%s/github-app/exchange-code", g.AtlantisURL),
		SetupURL:      fmt.Sprintf("%s/github-app/exchange-code", g.AtlantisURL),
		SetupOnUpdate: true,
		Public:        false,
		WebhookURL:    fmt.Sprintf("%s/events", g.AtlantisURL),
		WebhookSecret: secret,
		Events: []string{
			"check_run",
			"create",
			"delete",
			"pull_request",
			"push",
			"issue",
		},
		Checks:          "write",
		Contents:        "write",
		Issues:          "write",
		PullRequests:    "write",
		RepositoryHooks: "write",
		Statuses:        "write",
	})

	url := &url.URL{
		Scheme:   "https",
		Host:     g.GithubHostname,
		RawQuery: q.Encode(),
		Path:     "/settings/apps/new",
	}

	// https://developer.github.com/apps/building-github-apps/creating-github-apps-using-url-parameters/#about-github-app-url-parameters
	if g.GithubOrg != "" {
		url.Path = fmt.Sprintf("organizations/%s%s", g.GithubOrg, url.Path)
	}

	w.Header().Add("Location", url.String())
	g.respond(w, logging.Info, http.StatusTemporaryRedirect, "Redirecting to GitHub...")
}

func (g *GithubAppController) respond(w http.ResponseWriter, lvl logging.LogLevel, code int, format string, args ...interface{}) {
	response := fmt.Sprintf(format, args...)
	g.Logger.Log(lvl, response)
	w.WriteHeader(code)
	fmt.Fprintln(w, response)
}

func (g *GithubAppController) newWebhookSecret(length int) (string, error) {
	bytes := make([]byte, length)
	_, err := rand.Read(bytes)

	if err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(bytes), nil
}
